package core

import "errors"

var (
	ErrNotFound                 = errors.New("not found")
	ErrBindingNotFound          = errors.New("binding not found")
	ErrSessionSelectionRequired = errors.New("session selection required")
	ErrInvalidInput             = errors.New("invalid input")
	ErrExecutionUnavailable     = errors.New("execution unavailable")
	ErrRunActive                = errors.New("run is active")
)
