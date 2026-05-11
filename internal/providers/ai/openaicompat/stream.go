package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var text strings.Builder
	toolCalls := map[int]*streamToolCall{}
	var usage providers.Usage
	if err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "[DONE]" {
			return nil
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("openaicompat: decode stream chunk: %w", err)
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = openAIUsage(chunk.Usage)
		}
		if len(chunk.Choices) == 0 {
			return nil
		}

		deltaChunk := chunk.Choices[0].Delta
		for _, toolCall := range deltaChunk.ToolCalls {
			mergeStreamToolCall(toolCalls, toolCall)
		}

		delta := deltaChunk.Content
		if delta == "" {
			delta = chunk.Choices[0].Message.Content
		}
		if delta == "" {
			return nil
		}

		text.WriteString(delta)
		return providers.StreamText(ctx, delta)
	}); err != nil {
		return providers.Response{}, err
	}

	reply := strings.TrimSpace(text.String())
	calls := streamToolCalls(toolCalls)
	if reply == "" && len(calls) == 0 {
		return providers.Response{}, errors.New("openaicompat: empty assistant reply")
	}
	return providers.Response{
		Text:      reply,
		Model:     r.model,
		Provider:  providers.TypeOpenAICompat,
		ToolCalls: calls,
		Usage:     usage,
	}, nil
}

type streamToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func mergeStreamToolCall(calls map[int]*streamToolCall, delta chatCompletionToolCallDelta) {
	call := calls[delta.Index]
	if call == nil {
		call = &streamToolCall{}
		calls[delta.Index] = call
	}
	if id := strings.TrimSpace(delta.ID); id != "" {
		call.id = id
	}
	if name := strings.TrimSpace(delta.Function.Name); name != "" {
		call.name = name
	}
	if delta.Function.Arguments != "" {
		call.arguments.WriteString(delta.Function.Arguments)
	}
}

func streamToolCalls(calls map[int]*streamToolCall) []providers.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]providers.ToolCall, 0, len(calls))
	for i := 0; i < len(calls); i++ {
		call := calls[i]
		if call == nil || strings.TrimSpace(call.name) == "" {
			continue
		}
		out = append(out, providers.ToolCall{
			ID:        strings.TrimSpace(call.id),
			Name:      strings.TrimSpace(call.name),
			Arguments: compactJSONRaw(call.arguments.String()),
		})
	}
	return out
}
