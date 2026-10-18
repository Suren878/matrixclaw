package providers

import (
	"context"
	"errors"
	"io"
	"net"
	"time"
)

var (
	ErrEmptyResponse      = errors.New("empty assistant reply")
	ErrIncompleteResponse = errors.New("provider stream ended before completion")
	ErrMalformedToolCall  = errors.New("model produced a malformed tool call")
	// ErrReauthRequired marks credentials only a new sign-in can fix.
	ErrReauthRequired = errors.New("sign-in expired")
)

// ClassifyError says what a generation failure means for a retry: its kind
// and the wait the server asked for (0 if none). A canceled context and
// application errors are permanent.
func ClassifyError(err error) (ErrorKind, time.Duration) {
	var apiErr *APIError
	switch {
	case err == nil:
		return "", 0
	case errors.Is(err, context.Canceled):
		return ErrorPermanent, 0
	case errors.As(err, &apiErr):
		return apiErr.Kind, apiErr.RetryAfter
	case errors.Is(err, ErrReauthRequired):
		return ErrorAuth, 0
	case errors.Is(err, ErrEmptyResponse) || errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrMalformedToolCall) ||
		errors.Is(err, ErrStreamIdle) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
		return ErrorTransient, 0
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return ErrorTransient, 0
	}
	return ErrorPermanent, 0
}

// IsContextOverflow reports a request over the model window. Providers that
// only describe it in an error event's text are recognised by its wording.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Kind == ErrorContextOverflow
	}
	return IsContextOverflowText(err.Error())
}
