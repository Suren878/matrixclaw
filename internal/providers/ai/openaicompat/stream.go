package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var text strings.Builder
	var reasoningContent strings.Builder
	reasoningContentSeen := false
	toolCalls := map[int]*streamToolCall{}
	var usage providers.Usage
	completed := false
	finishReason := ""
	if err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "[DONE]" {
			completed = true
			return providers.ErrSSEComplete
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("openaicompat: decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("openaicompat: stream error: %s", chunk.Error.Message)
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = openAIUsage(chunk.Usage)
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		if reason := chunk.Choices[0].FinishReason; reason != "" {
			finishReason = reason
			completed = true
		}

		deltaChunk := chunk.Choices[0].Delta
		for _, toolCall := range deltaChunk.ToolCalls {
			mergeStreamToolCall(toolCalls, toolCall)
		}
		if deltaChunk.ReasoningContent != nil {
			reasoningContentSeen = true
			reasoningContent.WriteString(*deltaChunk.ReasoningContent)
		}

		delta := deltaChunk.Content
		if delta == "" {
			delta = chunk.Choices[0].Message.Content
		}
		if chunk.Choices[0].Message.ReasoningContent != nil {
			reasoningContentSeen = true
			reasoningContent.WriteString(*chunk.Choices[0].Message.ReasoningContent)
		}
		if delta == "" {
			return nil
		}

		text.WriteString(delta)
		return providers.StreamText(ctx, delta)
	}); err != nil {
		return providers.Response{}, err
	}
	if !completed {
		return providers.Response{}, fmt.Errorf("openaicompat: %w", providers.ErrIncompleteResponse)
	}

	var responseReasoningContent *string
	if reasoningContentSeen {
		value := reasoningContent.String()
		responseReasoningContent = &value
	}
	return r.finishResponse(text.String(), responseReasoningContent, streamToolCalls(toolCalls), finishReason, usage)
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
	indices := make([]int, 0, len(calls))
	for index := range calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, i := range indices {
		call := calls[i]
		if call == nil {
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
