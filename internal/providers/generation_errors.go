package providers

import (
	"context"
	"errors"
	"io"
	"net"
)

var (
	ErrEmptyResponse      = errors.New("empty assistant reply")
	ErrIncompleteResponse = errors.New("provider stream ended before completion")
)

// IsRetryableGenerationError is for failures before any output or tool effects.
// Authentication, invalid requests, and application errors are not retried.
func IsRetryableGenerationError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, ErrEmptyResponse) || errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrStreamIdle) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}
