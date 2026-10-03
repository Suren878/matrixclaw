package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage anthropicUsagePayload `json:"usage"`
	} `json:"message"`
	ContentBlock anthropicBlock `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage anthropicUsagePayload `json:"usage"`
	Error *anthropicErrorDetail `json:"error,omitempty"`
}

// streamBlock accumulates one content block across its deltas.
type streamBlock struct {
	block     anthropicBlock
	text      strings.Builder
	input     strings.Builder
	signature strings.Builder
}

func (b *streamBlock) final() anthropicBlock {
	block := b.block
	switch block.Type {
	case "text":
		block.Text = b.text.String()
	case "thinking":
		thinking := b.text.String()
		block.Thinking = &thinking
		block.Signature += b.signature.String()
	case "tool_use":
		if b.input.Len() > 0 {
			block.Input = json.RawMessage(b.input.String())
		}
	}
	return block
}

func streamDeltaBlockType(deltaType string) string {
	switch deltaType {
	case "", "text_delta":
		return "text"
	case "input_json_delta":
		return "tool_use"
	case "thinking_delta", "signature_delta":
		return "thinking"
	default:
		return ""
	}
}

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	blocks := map[int]*streamBlock{}
	var usage anthropicUsagePayload
	stopReason := ""
	completed := false
	err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "" {
			return nil
		}
		var chunk anthropicStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("anthropic: decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return anthropicStreamError(*chunk.Error)
		}
		eventType := event.Type
		if eventType == "" {
			eventType = chunk.Type
		}
		switch eventType {
		case "message_start":
			usage = chunk.Message.Usage
		case "content_block_start":
			started := &streamBlock{block: chunk.ContentBlock}
			blocks[chunk.Index] = started
			switch chunk.ContentBlock.Type {
			case "text":
				started.text.WriteString(chunk.ContentBlock.Text)
				return providers.StreamText(ctx, chunk.ContentBlock.Text)
			case "thinking":
				if chunk.ContentBlock.Thinking != nil {
					started.text.WriteString(*chunk.ContentBlock.Thinking)
				}
			}
		case "content_block_delta":
			blockType := streamDeltaBlockType(chunk.Delta.Type)
			if blockType == "" {
				return nil
			}
			block := blocks[chunk.Index]
			if block == nil {
				block = &streamBlock{block: anthropicBlock{Type: blockType}}
				blocks[chunk.Index] = block
			}
			switch chunk.Delta.Type {
			case "input_json_delta":
				block.input.WriteString(chunk.Delta.PartialJSON)
			case "thinking_delta":
				block.text.WriteString(chunk.Delta.Thinking)
			case "signature_delta":
				block.signature.WriteString(chunk.Delta.Signature)
			default:
				block.text.WriteString(chunk.Delta.Text)
				return providers.StreamText(ctx, chunk.Delta.Text)
			}
		case "message_delta":
			usage = mergeAnthropicUsage(usage, chunk.Usage)
			if chunk.Delta.StopReason != "" {
				stopReason = chunk.Delta.StopReason
			}
		case "message_stop":
			completed = true
			return providers.ErrSSEComplete
		}
		return nil
	})
	if err != nil {
		return providers.Response{}, err
	}
	if !completed {
		return providers.Response{}, fmt.Errorf("anthropic: %w", providers.ErrIncompleteResponse)
	}
	indices := make([]int, 0, len(blocks))
	for index := range blocks {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	final := make([]anthropicBlock, 0, len(indices))
	for _, index := range indices {
		final = append(final, blocks[index].final())
	}
	return r.assembleResponse(final, stopReason, usage)
}
