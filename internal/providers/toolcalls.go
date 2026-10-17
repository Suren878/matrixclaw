package providers

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CheckToolCalls fails a reply with a tool call that has no name or whose
// arguments are not valid JSON, so the generation is retried.
func CheckToolCalls(adapter string, calls []ToolCall) error {
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			return fmt.Errorf("%s: function call is missing a name: %w", adapter, ErrMalformedToolCall)
		}
		if !json.Valid(call.Arguments) {
			return fmt.Errorf("%s: invalid arguments for tool %q: %w", adapter, call.Name, ErrMalformedToolCall)
		}
	}
	return nil
}
