package core

import (
	"errors"
	"strings"
)

func isContextLengthExceededError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for wrapped := errors.Unwrap(err); wrapped != nil; wrapped = errors.Unwrap(wrapped) {
		text += " " + strings.ToLower(wrapped.Error())
	}
	if strings.Contains(text, "context_length_exceeded") ||
		strings.Contains(text, "context length exceeded") ||
		strings.Contains(text, "maximum context length") ||
		strings.Contains(text, "context window") && strings.Contains(text, "exceed") ||
		strings.Contains(text, "exceeds context") ||
		strings.Contains(text, "exceeds the context") ||
		strings.Contains(text, "too many tokens") ||
		strings.Contains(text, "input is too long") ||
		strings.Contains(text, "request too large") {
		return true
	}
	return strings.Contains(text, "token") && strings.Contains(text, "limit") && strings.Contains(text, "exceed")
}
