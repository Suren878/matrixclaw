package agentcontext

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

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

// IsContextLengthExceeded recognises provider errors for requests over the model window.
func IsContextLengthExceeded(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for wrapped := errors.Unwrap(err); wrapped != nil; wrapped = errors.Unwrap(wrapped) {
		text += " " + strings.ToLower(wrapped.Error())
	}
	for _, phrase := range overflowPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return strings.Contains(text, "context window") && strings.Contains(text, "exceed") ||
		strings.Contains(text, "token") && strings.Contains(text, "limit") && strings.Contains(text, "exceed")
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
