package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) decodeResponse(body []byte) (providers.Response, error) {
	var response anthropicResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	return r.assembleResponse(response.Content, response.StopReason, response.Usage)
}

// assembleResponse turns final content blocks into a provider response. A tool
// call cut by the output limit is dropped; any other malformed call is an error.
func (r *Runtime) assembleResponse(blocks []anthropicBlock, rawStopReason string, usage anthropicUsagePayload) (providers.Response, error) {
	stop := anthropicStopReason(rawStopReason)
	var text strings.Builder
	var calls []providers.ToolCall
	var reasoning []providers.ReasoningBlock
	for _, block := range blocks {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "thinking":
			thinking := ""
			if block.Thinking != nil {
				thinking = *block.Thinking
			}
			reasoning = append(reasoning, providers.ReasoningBlock{Text: thinking, Signature: block.Signature})
		case "redacted_thinking":
			reasoning = append(reasoning, providers.ReasoningBlock{RedactedData: block.Data})
		case "tool_use":
			call, err := decodeToolUse(block)
			if err != nil {
				if stop == providers.StopMaxTokens {
					continue
				}
				return providers.Response{}, err
			}
			calls = append(calls, call)
		}
	}
	stop = providers.ResolveStopReason(stop, len(calls))
	reply := text.String()
	if strings.TrimSpace(reply) == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("anthropic: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:       reply,
		Model:      r.Model,
		Provider:   providers.TypeAnthropic,
		Reasoning:  reasoning,
		ToolCalls:  calls,
		StopReason: stop,
		Usage:      anthropicUsage(usage),
	}, nil
}

func decodeToolUse(block anthropicBlock) (providers.ToolCall, error) {
	name := strings.TrimSpace(block.Name)
	if name == "" {
		return providers.ToolCall{}, fmt.Errorf("anthropic: tool call %q has no name: %w", block.ID, providers.ErrMalformedToolCall)
	}
	input := bytes.TrimSpace(block.Input)
	if len(input) == 0 {
		input = []byte(`{}`)
	}
	if !json.Valid(input) {
		return providers.ToolCall{}, fmt.Errorf("anthropic: tool call %q has malformed input JSON: %w", name, providers.ErrMalformedToolCall)
	}
	return providers.ToolCall{ID: strings.TrimSpace(block.ID), Name: name, Arguments: json.RawMessage(input)}, nil
}
