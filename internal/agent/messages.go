package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ToolCallMessage is the transcript message of one requested tool call.
func ToolCallMessage(id, sessionID, runID, name string, args []byte, finished bool, at time.Time) transcript.Message {
	return transcript.Message{
		ID:        id,
		SessionID: sessionID,
		RunID:     runID,
		Role:      transcript.MessageRoleAssistant,
		Parts: []transcript.MessagePart{{
			Kind:     transcript.MessagePartKindToolCall,
			ToolCall: &transcript.ToolCallPart{ID: id, Name: name, Input: string(args), Finished: finished},
		}},
		CreatedAt: at,
		UpdatedAt: at,
	}
}

// ToolResultMessage is the transcript message carrying a tool result.
func ToolResultMessage(id, sessionID, runID, callID, name string, result tools.Result, at time.Time) (transcript.Message, error) {
	var metadata json.RawMessage
	if result.Metadata != nil {
		body, err := json.Marshal(result.Metadata)
		if err != nil {
			return transcript.Message{}, err
		}
		metadata = body
	}
	content := strings.TrimSpace(result.Content)
	return transcript.Message{
		ID:        id,
		SessionID: sessionID,
		RunID:     runID,
		Role:      transcript.MessageRoleTool,
		Content:   normalizeToolContent(content),
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindToolResult,
			ToolResult: &transcript.ToolResultPart{
				ToolCallID: callID,
				Name:       name,
				Content:    content,
				MIMEType:   result.MIMEType,
				Metadata:   metadata,
				Status:     string(ToolResultStatus(result)),
				IsError:    result.IsError,
				OutputPath: result.OutputPath,
			},
		}},
		CreatedAt: at,
		UpdatedAt: at,
	}, nil
}

// ToolResultStatus is the stored status of a tool result.
func ToolResultStatus(result tools.Result) tools.ResultStatus {
	if result.Status != "" {
		return result.Status
	}
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func normalizeToolContent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Tool completed"
	}
	return value
}

// finalReply is the persisted form of a completed text reply: text, plain
// reasoning_content and usage (signed reasoning is kept only on tool steps).
func finalReply(assistant transcript.Message, response providers.Response) transcript.Message {
	assistant.Content = response.Text
	assistant.Parts = transcript.NormalizeMessageParts(assistant.Content, nil)
	if response.ReasoningContent != nil {
		assistant.Parts = append(assistant.Parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: *response.ReasoningContent}})
	}
	if finish := usageFinishPart(response.Usage); finish != nil {
		assistant.Parts = append(assistant.Parts, *finish)
	}
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	return assistant
}

// reasoningParts keeps the reasoning a provider needs back with this
// tool step: plain reasoning_content text and signed or encrypted blocks.
func reasoningParts(response providers.Response) []transcript.MessagePart {
	var parts []transcript.MessagePart
	if response.ReasoningContent != nil {
		parts = append(parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: *response.ReasoningContent}})
	}
	for _, block := range response.Reasoning {
		parts = append(parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: block.Text, Signature: block.Signature, RedactedData: block.RedactedData}})
	}
	return parts
}

func usageFinishPart(usage providers.Usage) *transcript.MessagePart {
	if usage.IsZero() {
		return nil
	}
	payload, err := json.Marshal(struct {
		Usage providers.Usage `json:"usage"`
	}{Usage: usage})
	if err != nil {
		return nil
	}
	return &transcript.MessagePart{
		Kind: transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{
			Reason:  "end_turn",
			Details: payload,
		},
	}
}
