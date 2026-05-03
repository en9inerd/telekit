package telekit

import (
	"testing"
)

func TestParseParams_NoSchema(t *testing.T) {
	got, err := parseParams("/cmd a=1 b=hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.String("a") != "1" || got.String("b") != "hello" {
		t.Errorf("got %v", got)
	}
}

func TestParseParams_TypedSchema(t *testing.T) {
	schema := Params{
		"name":    {Type: TypeString, Required: true},
		"count":   {Type: TypeInt, Required: true},
		"verbose": {Type: TypeBool},
	}
	got, err := parseParams("/cmd name=test count=42 verbose=true", schema)
	if err != nil {
		t.Fatal(err)
	}
	if got.String("name") != "test" {
		t.Errorf("name = %q", got.String("name"))
	}
	if got.Int("count") != 42 {
		t.Errorf("count = %d", got.Int("count"))
	}
	if !got.Bool("verbose") {
		t.Error("verbose should be true")
	}
}

func TestParseParams_MissingRequired(t *testing.T) {
	schema := Params{
		"name": {Type: TypeString, Required: true},
	}
	_, err := parseParams("/cmd", schema)
	if err == nil {
		t.Error("expected error for missing required param")
	}
}

func TestParseParams_InvalidInt(t *testing.T) {
	schema := Params{
		"n": {Type: TypeInt},
	}
	_, err := parseParams("/cmd n=abc", schema)
	if err == nil {
		t.Error("expected error for invalid int")
	}
}

func TestParseParams_InvalidBool(t *testing.T) {
	schema := Params{
		"flag": {Type: TypeBool},
	}
	_, err := parseParams("/cmd flag=maybe", schema)
	if err == nil {
		t.Error("expected error for invalid bool")
	}
}

func TestParseParams_Enum(t *testing.T) {
	schema := Params{
		"color": {Type: TypeEnum, Enum: []string{"red", "blue"}},
	}
	got, err := parseParams("/cmd color=red", schema)
	if err != nil {
		t.Fatal(err)
	}
	if got.String("color") != "red" {
		t.Errorf("color = %q", got.String("color"))
	}

	_, err = parseParams("/cmd color=green", schema)
	if err == nil {
		t.Error("expected error for invalid enum value")
	}
}

func TestParseParams_Default(t *testing.T) {
	schema := Params{
		"n": {Type: TypeInt, Default: int64(10)},
	}
	got, err := parseParams("/cmd", schema)
	if err != nil {
		t.Fatal(err)
	}
	if got.Int("n") != 10 {
		t.Errorf("n = %d, want 10", got.Int("n"))
	}
}

func TestParseParams_UnknownParam(t *testing.T) {
	schema := Params{
		"name": {Type: TypeString},
	}
	_, err := parseParams("/cmd name=ok bogus=bad", schema)
	if err == nil {
		t.Error("expected error for unknown param")
	}
}

func TestParseParams_Empty(t *testing.T) {
	got, err := parseParams("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestParsedParams_Has(t *testing.T) {
	p := ParsedParams{"key": "val"}
	if !p.Has("key") {
		t.Error("Has(key) should be true")
	}
	if p.Has("missing") {
		t.Error("Has(missing) should be false")
	}
}
