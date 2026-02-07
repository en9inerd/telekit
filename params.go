package telekit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ParamType defines the type of a command parameter.
type ParamType string

const (
	TypeString ParamType = "string"
	TypeInt    ParamType = "int"
	TypeBool   ParamType = "bool"
	TypeEnum   ParamType = "enum"
)

// ParamSchema defines validation rules for a command parameter.
type ParamSchema struct {
	// Type is the parameter type (string, int, bool, enum).
	Type ParamType

	// Required indicates if the parameter must be provided.
	Required bool

	// Default is the default value if not provided.
	Default any

	// Enum contains allowed values for enum type.
	Enum []string

	// Description is a human-readable description for help text.
	Description string
}

// Params is a map of parameter names to their schemas.
type Params map[string]ParamSchema

// ParsedParams holds validated parameter values.
type ParsedParams map[string]any

// String returns the string value of a parameter.
func (p ParsedParams) String(key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

// Int returns the int64 value of a parameter.
func (p ParsedParams) Int(key string) int64 {
	if v, ok := p[key].(int64); ok {
		return v
	}
	return 0
}

// Bool returns the bool value of a parameter.
func (p ParsedParams) Bool(key string) bool {
	if v, ok := p[key].(bool); ok {
		return v
	}
	return false
}

// Has returns true if the parameter was provided.
func (p ParsedParams) Has(key string) bool {
	_, ok := p[key]
	return ok
}

// parseParams parses command text and validates against schema.
// Command format: /command key1=value1 key2=value2
func parseParams(text string, schema Params) (ParsedParams, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil, nil
	}

	raw := make(map[string]string)
	for _, part := range parts[1:] {
		if idx := strings.Index(part, "="); idx > 0 {
			key := part[:idx]
			value := part[idx+1:]
			raw[key] = value
		}
	}

	if schema == nil {
		params := make(ParsedParams)
		for k, v := range raw {
			params[k] = v
		}
		return params, nil
	}

	params := make(ParsedParams)
	var errs []string

	for name, s := range schema {
		rawValue, provided := raw[name]

		if s.Required && !provided {
			errs = append(errs, fmt.Sprintf("parameter %q is required", name))
			continue
		}

		if !provided {
			if s.Default != nil {
				params[name] = s.Default
			}
			continue
		}

		switch s.Type {
		case TypeString:
			params[name] = rawValue

		case TypeInt:
			n, err := strconv.ParseInt(rawValue, 10, 64)
			if err != nil {
				errs = append(errs, fmt.Sprintf("parameter %q must be a number", name))
			} else {
				params[name] = n
			}

		case TypeBool:
			switch strings.ToLower(rawValue) {
			case "true", "1", "yes":
				params[name] = true
			case "false", "0", "no":
				params[name] = false
			default:
				errs = append(errs, fmt.Sprintf("parameter %q must be true or false", name))
			}

		case TypeEnum:
			if slices.Contains(s.Enum, rawValue) {
				params[name] = rawValue
			} else {
				errs = append(errs, fmt.Sprintf("parameter %q must be one of: %s",
					name, strings.Join(s.Enum, ", ")))
			}

		default:
			params[name] = rawValue
		}
	}

	for name := range raw {
		if _, exists := schema[name]; !exists {
			errs = append(errs, fmt.Sprintf("unknown parameter %q", name))
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	return params, nil
}
