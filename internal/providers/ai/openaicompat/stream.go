package openaicompat

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var text strings.Builder
	var reasoningContent strings.Builder
	reasoningContentSeen := false
	var toolCalls streamToolCalls
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
			return providers.NewStreamError("openaicompat", chunk.Error.status(), "stream error: "+chunk.Error.Message)
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
			toolCalls.merge(toolCall)
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
	return r.finishResponse(text.String(), responseReasoningContent, toolCalls.toolCalls(), finishReason, usage)
}

type streamToolCall struct {
	index     int
	id        string
	name      string
	arguments strings.Builder
}

// streamToolCalls gathers tool-call deltas by index. Some gateways leave the
// index out or reuse one; a new id then starts a new call, after the others.
type streamToolCalls struct {
	calls   []*streamToolCall
	byIndex map[int]*streamToolCall
	last    *streamToolCall
}

func (s *streamToolCalls) merge(delta chatCompletionToolCallDelta) {
	call := s.last
	if delta.Index != nil {
		call = s.byIndex[*delta.Index]
	}
	id := strings.TrimSpace(delta.ID)
	if call == nil || id != "" && call.id != "" && id != call.id {
		call = &streamToolCall{index: s.nextIndex()}
		if delta.Index != nil {
			call.index = *delta.Index
		}
		s.calls = append(s.calls, call)
	}
	if delta.Index != nil {
		if s.byIndex == nil {
			s.byIndex = map[int]*streamToolCall{}
		}
		s.byIndex[*delta.Index] = call
	}
	s.last = call
	if id != "" {
		call.id = id
	}
	if name := strings.TrimSpace(delta.Function.Name); name != "" {
		call.name = name
	}
	if delta.Function.Arguments != "" {
		call.arguments.WriteString(delta.Function.Arguments)
	}
}

func (s *streamToolCalls) nextIndex() int {
	next := 0
	for _, call := range s.calls {
		next = max(next, call.index+1)
	}
	return next
}

func (s *streamToolCalls) toolCalls() []providers.ToolCall {
	if len(s.calls) == 0 {
		return nil
	}
	slices.SortStableFunc(s.calls, func(a, b *streamToolCall) int { return cmp.Compare(a.index, b.index) })
	out := make([]providers.ToolCall, 0, len(s.calls))
	for _, call := range s.calls {
		out = append(out, providers.ToolCall{
			ID:        strings.TrimSpace(call.id),
			Name:      strings.TrimSpace(call.name),
			Arguments: toolArguments(call.arguments.String()),
		})
	}
	return out
}

// chunkError is an error inside a stream; OpenRouter's code is the HTTP
// status, OpenAI's a name such as "rate_limit_exceeded".
type chunkError struct {
	Code    json.RawMessage `json:"code"`
	Type    string          `json:"type"`
	Message string          `json:"message"`
}

func (e chunkError) status() int {
	var status int
	if json.Unmarshal(e.Code, &status) == nil {
		return status
	}
	var name string
	_ = json.Unmarshal(e.Code, &name)
	return providers.OpenAIErrorStatus(textutil.FirstNonEmpty(name, e.Type))
}
