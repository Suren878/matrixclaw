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
	stop, err := responsesStopReason(response)
	if err != nil {
		return providers.Response{}, err
	}
	var texts []string
	var calls []providers.ToolCall
	var reasoning []providers.ReasoningBlock
	refused := false
	for _, item := range response.Output {
		switch item.Type {
		case "reasoning":
			if item.EncryptedContent != "" {
				reasoning = append(reasoning, providers.ReasoningBlock{Text: reasoningSummary(item.Summary), RedactedData: item.EncryptedContent})
			}
		case "message":
			var text strings.Builder
			for _, part := range item.Content {
				switch part.Type {
				case "output_text", "text":
					text.WriteString(part.Text)
				case "refusal":
					refused = true
					text.WriteString(part.Refusal)
				}
			}
			if value := text.String(); strings.TrimSpace(value) != "" {
				texts = append(texts, value)
			}
		case "function_call":
			name := strings.TrimSpace(item.Name)
			id := strings.TrimSpace(item.CallID)
			arguments := responsesToolArguments(item.Arguments)
			if stop == providers.StopMaxTokens && (name == "" || id == "" || !json.Valid(arguments)) {
				continue // cut off by the output limit
			}
			if name == "" || id == "" {
				return providers.Response{}, fmt.Errorf("openai-codex: function call is missing name or call_id")
			}
			if !json.Valid(arguments) {
				return providers.Response{}, fmt.Errorf("openai-codex: invalid arguments for tool %q", name)
			}
			calls = append(calls, providers.ToolCall{ID: id, Name: name, Arguments: arguments})
		}
	}
	if refused && stop == providers.StopEndTurn && len(calls) == 0 {
		stop = providers.StopRefusal
	}
	stop = providers.ResolveStopReason(stop, len(calls))
	text := strings.Join(texts, "\n\n")
	if text == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("openai-codex: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{Text: text, ToolCalls: calls, Model: r.model, Provider: providers.TypeOpenAICodex, StopReason: stop, Reasoning: reasoning, Usage: response.Usage.toProviderUsage()}, nil
}

func reasoningSummary(parts []responsesSummaryPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n")
}

// responsesStopReason maps the terminal status; an incomplete response stays
// usable only when the output limit or a content filter ended it.
func responsesStopReason(response responsesResponse) (providers.StopReason, error) {
	reason := ""
	if response.IncompleteDetails != nil {
		reason = response.IncompleteDetails.Reason
	}
	switch {
	case response.Status != "" && response.Status != "completed" && response.Status != "incomplete":
		if reason != "" {
			return "", fmt.Errorf("openai-codex: response %s: %s", response.Status, reason)
		}
		return "", fmt.Errorf("openai-codex: response %s", response.Status)
	case response.Status != "incomplete" && response.IncompleteDetails == nil:
		return providers.StopEndTurn, nil
	case reason == "max_output_tokens":
		return providers.StopMaxTokens, nil
	case reason == "content_filter":
		return providers.StopContentFilter, nil
	default:
		return "", fmt.Errorf("openai-codex: incomplete response: %s", firstNonEmpty(reason, "no reason given"))
	}
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
		case "response.failed", "response.cancelled":
			// Some gateways omit response.status on terminal failure events.
			chunk.Response.Status = strings.TrimPrefix(firstNonEmpty(chunk.Type, event.Type), "response.")
			_, err := r.completedResponse(chunk.Response)
			return err
		case "response.incomplete":
			final = chunk.Response
			final.Status = "incomplete"
			completed = true
			return providers.ErrSSEComplete
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
