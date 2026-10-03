package providers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrorKind says what a provider error means for the caller.
type ErrorKind string

const (
	ErrorRateLimit       ErrorKind = "rate_limit"
	ErrorOverloaded      ErrorKind = "overloaded"
	ErrorContextOverflow ErrorKind = "context_overflow"
	ErrorAuth            ErrorKind = "auth"
	ErrorInvalid         ErrorKind = "invalid"
)

// APIError is an error the provider reported, over HTTP or as a stream event
// (Status 0). RetryAfter is the server's requested wait, if it gave one.
type APIError struct {
	Provider   string
	Status     int
	Kind       ErrorKind
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	switch {
	case e.Status == 0:
		return e.Provider + ": " + e.Message
	case e.Message == "":
		return fmt.Sprintf("%s: status %d", e.Provider, e.Status)
	default:
		return fmt.Sprintf("%s: status %d: %s", e.Provider, e.Status, e.Message)
	}
}

// Retryable reports a failure that may pass when the same request is sent again.
func (e *APIError) Retryable() bool {
	return e.Kind == ErrorRateLimit || e.Kind == ErrorOverloaded
}

// NewAPIError classifies an HTTP error response; header may be nil.
func NewAPIError(provider string, status int, message string, header http.Header) *APIError {
	err := &APIError{Provider: provider, Status: status, Message: strings.TrimSpace(message), Kind: statusErrorKind(status, message)}
	if header != nil {
		err.RetryAfter = RetryAfterDelay(header.Get("Retry-After"), time.Now(), 0)
	}
	return err
}

// NewStreamError classifies an error event inside a stream; code is the
// HTTP-style status the event carries, or 0.
func NewStreamError(provider string, code int, message string) *APIError {
	return &APIError{Provider: provider, Kind: statusErrorKind(code, message), Message: strings.TrimSpace(message)}
}

func statusErrorKind(status int, message string) ErrorKind {
	switch {
	case status == http.StatusTooManyRequests:
		return ErrorRateLimit
	case status == http.StatusRequestTimeout || status >= 500:
		return ErrorOverloaded
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrorAuth
	case IsContextOverflowText(message):
		return ErrorContextOverflow
	default:
		return ErrorInvalid
	}
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

// overflowPhrases are how providers word a request over the model window:
// OpenAI, OpenRouter, DeepSeek and Mistral ("maximum context length"),
// Anthropic ("prompt is too long"), Gemini, xAI and OpenAI Responses.
var overflowPhrases = []string{
	"context_length_exceeded",
	"context length exceeded",
	"maximum context length",
	"prompt is too long",
	"exceeds the maximum number of tokens",
	"maximum prompt length",
	"exceeds context",
	"exceeds the context",
	"too many tokens",
	"input is too long",
	"request too large",
}

// rateLimitPhrases mark a request over a rate limit, such as tokens per
// minute, which providers word much like a request over the window.
var rateLimitPhrases = []string{"per min", "tpm", "rate limit", "rate_limit"}

// IsContextOverflowText recognises a provider's wording of a request over the window.
func IsContextOverflowText(text string) bool {
	text = strings.ToLower(text)
	for _, phrase := range rateLimitPhrases {
		if strings.Contains(text, phrase) {
			return false
		}
	}
	for _, phrase := range overflowPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return strings.Contains(text, "context window") && strings.Contains(text, "exceed") ||
		strings.Contains(text, "token") && strings.Contains(text, "limit") && strings.Contains(text, "exceed")
}
