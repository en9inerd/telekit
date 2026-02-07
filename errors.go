package telekit

import "errors"

// Configuration errors
var (
	ErrMissingAPIID    = errors.New("telekit: API ID is required")
	ErrMissingAPIHash  = errors.New("telekit: API hash is required")
	ErrMissingBotToken = errors.New("telekit: bot token is required")
)

// Runtime errors
var (
	ErrBotNotRunning  = errors.New("telekit: bot is not running")
	ErrAlreadyRunning = errors.New("telekit: bot is already running")
)
