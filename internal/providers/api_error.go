package providers

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrorKind says what a provider error means for the caller.
type ErrorKind string

const (
	ErrorRateLimit       ErrorKind = "rate_limit" // too many requests now; wait RetryAfter
	ErrorOverloaded      ErrorKind = "overloaded" // server busy or failing
	ErrorTransient       ErrorKind = "transient"  // network or stream cut before the reply ended
	ErrorAuth            ErrorKind = "auth"       // bad or expired credentials
	ErrorQuota           ErrorKind = "quota"      // out of credit or plan usage; waiting minutes will not help
	ErrorContextOverflow ErrorKind = "context_overflow"
	ErrorPermanent       ErrorKind = "permanent" // the same request fails again
)

// Retryable reports a kind that may pass when the same request is sent again.
func (k ErrorKind) Retryable() bool {
	return k == ErrorRateLimit || k == ErrorOverloaded || k == ErrorTransient
}

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
	return e.Kind.Retryable()
}

// NewAPIError classifies an HTTP error response. code is the error code or
// type the body names ("" if none); header may be nil.
func NewAPIError(provider string, status int, code string, message string, header http.Header) *APIError {
	err := &APIError{Provider: provider, Status: status, Message: strings.TrimSpace(message)}
	if header != nil {
		err.RetryAfter = retryAfter(header, time.Now())
	}
	err.Kind = classify(status, code, err.Message)
	return err
}

// NewStreamError classifies an error event inside a stream; status is the
// HTTP-style status the event carries, or 0, and code its error code or type.
func NewStreamError(provider string, status int, code string, message string) *APIError {
	return &APIError{Provider: provider, Kind: classify(status, code, message), Message: strings.TrimSpace(message)}
}

// codeKinds are the error codes and types providers put in error bodies and
// stream events: OpenAI, Anthropic, Gemini status names, Codex.
var codeKinds = map[string]ErrorKind{
	"rate_limit_exceeded":        ErrorRateLimit,
	"rate_limit_error":           ErrorRateLimit,
	"resource_exhausted":         ErrorRateLimit,
	"server_error":               ErrorOverloaded,
	"server_is_overloaded":       ErrorOverloaded,
	"overloaded":                 ErrorOverloaded,
	"overloaded_error":           ErrorOverloaded,
	"service_unavailable":        ErrorOverloaded,
	"api_error":                  ErrorOverloaded,
	"unavailable":                ErrorOverloaded,
	"internal":                   ErrorOverloaded,
	"authentication_error":       ErrorAuth,
	"permission_error":           ErrorAuth,
	"invalid_api_key":            ErrorAuth,
	"unauthenticated":            ErrorAuth,
	"permission_denied":          ErrorAuth,
	"insufficient_quota":         ErrorQuota,
	"billing_hard_limit_reached": ErrorQuota,
	"usage_limit_reached":        ErrorQuota,
	"context_length_exceeded":    ErrorContextOverflow,
}

// quotaPhrases are how providers word an exhausted balance or plan.
var quotaPhrases = []string{
	"insufficient_quota",
	"credit balance is too low",
	"insufficient balance",
	"insufficient credits",
	"exceeded your current quota",
}

func classify(status int, code string, message string) ErrorKind {
	if kind, ok := codeKinds[strings.ToLower(strings.TrimSpace(code))]; ok {
		return kind
	}
	text := strings.ToLower(message)
	switch {
	case status == http.StatusPaymentRequired || containsAny(text, quotaPhrases):
		return ErrorQuota
	case status == http.StatusTooManyRequests:
		return ErrorRateLimit
	case status == http.StatusRequestTimeout || status >= 500:
		return ErrorOverloaded
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrorAuth
	case IsContextOverflowText(message):
		return ErrorContextOverflow
	default:
		return ErrorPermanent
	}
}

func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
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
	if containsAny(text, rateLimitPhrases) {
		return false
	}
	if containsAny(text, overflowPhrases) {
		return true
	}
	return strings.Contains(text, "context window") && strings.Contains(text, "exceed") ||
		strings.Contains(text, "token") && strings.Contains(text, "limit") && strings.Contains(text, "exceed")
}
