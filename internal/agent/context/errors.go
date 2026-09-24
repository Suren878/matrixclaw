package agentcontext

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// IsContextLengthExceeded recognises provider errors for requests over the model window.
func IsContextLengthExceeded(err error) bool {
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

// stopReasonError fails a summary cut by the output limit or a content filter.
func stopReasonError(response providers.Response) error {
	switch response.StopReason {
	case providers.StopMaxTokens, providers.StopContentFilter:
		return fmt.Errorf("%s: generation stopped before completion (%s)", response.Provider, response.StopReason)
	default:
		return nil
	}
}
