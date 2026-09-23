package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) decodeResponse(raw []byte) (providers.Response, error) {
	var response responsesResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: decode response: %w", err)
	}
	return r.completedResponse(response)
}

// The completed response is authoritative for text, tools, order, and usage.
// Deltas are previews only; dispatching reconstructed partial tools risks
// executing unfinished arguments or losing calls present only in final output.
func (r *Runtime) completedResponse(response responsesResponse) (providers.Response, error) {
	if response.Error != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: %s", firstNonEmpty(response.Error.Message, response.Error.Code, "response failed"))
	}
	if response.Status != "" && response.Status != "completed" {
		reason := response.Status
		if response.IncompleteDetails != nil {
			reason += ": " + response.IncompleteDetails.Reason
		}
		return providers.Response{}, fmt.Errorf("openai-codex: response %s", reason)
	}
	if response.IncompleteDetails != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: incomplete response: %s", response.IncompleteDetails.Reason)
	}
	var texts []string
	var calls []providers.ToolCall
	for _, item := range response.Output {
		switch item.Type {
		case "message":
			var text strings.Builder
			for _, part := range item.Content {
				switch part.Type {
				case "output_text", "text":
					text.WriteString(part.Text)
				case "refusal":
					text.WriteString(part.Refusal)
				}
			}
			if value := strings.TrimSpace(text.String()); value != "" {
				texts = append(texts, value)
			}
		case "function_call":
			name := strings.TrimSpace(item.Name)
			id := strings.TrimSpace(item.CallID)
			if name == "" || id == "" {
				return providers.Response{}, fmt.Errorf("openai-codex: function call is missing name or call_id")
			}
			arguments := responsesToolArguments(item.Arguments)
			if !json.Valid(arguments) {
				return providers.Response{}, fmt.Errorf("openai-codex: invalid arguments for tool %q", name)
			}
			calls = append(calls, providers.ToolCall{ID: id, Name: name, Arguments: arguments})
		}
	}
	text := strings.Join(texts, "\n\n")
	if text == "" && len(calls) == 0 {
		return providers.Response{}, fmt.Errorf("openai-codex: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{Text: text, ToolCalls: calls, Model: r.model, Provider: providers.TypeOpenAICodex, Usage: response.Usage.toProviderUsage()}, nil
}

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var final responsesResponse
	completed := false
	err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "[DONE]" {
			return providers.ErrSSEComplete
		}
		var chunk responsesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("openai-codex: decode stream event: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("openai-codex: %s", firstNonEmpty(chunk.Error.Message, chunk.Error.Code, "stream error"))
		}
		switch firstNonEmpty(chunk.Type, event.Type) {
		case "response.output_text.delta", "response.refusal.delta":
			return providers.StreamText(ctx, chunk.Delta)
		case "error":
			return fmt.Errorf("openai-codex: %s", firstNonEmpty(chunk.Message, chunk.Code, "stream error"))
		case "response.failed", "response.incomplete", "response.cancelled":
			// Some gateways omit response.status on terminal failure events.
			chunk.Response.Status = strings.TrimPrefix(firstNonEmpty(chunk.Type, event.Type), "response.")
			_, err := r.completedResponse(chunk.Response)
			return err
		case "response.completed":
			final = chunk.Response
			completed = true
			return providers.ErrSSEComplete
		}
		return nil
	})
	if err != nil {
		return providers.Response{}, err
	}
	if !completed {
		return providers.Response{}, fmt.Errorf("openai-codex: %w", providers.ErrIncompleteResponse)
	}
	return r.completedResponse(final)
}
