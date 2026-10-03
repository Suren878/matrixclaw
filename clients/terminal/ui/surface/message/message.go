package message

import (
	"slices"
	"time"
)

type MessageRole string

const (
	Assistant MessageRole = "assistant"
	User      MessageRole = "user"
	System    MessageRole = "system"
	Tool      MessageRole = "tool"
)

type FinishReason string

const (
	FinishReasonEndTurn          FinishReason = "end_turn"
	FinishReasonMaxTokens        FinishReason = "max_tokens"
	FinishReasonToolUse          FinishReason = "tool_use"
	FinishReasonCanceled         FinishReason = "canceled"
	FinishReasonError            FinishReason = "error"
	FinishReasonPermissionDenied FinishReason = "permission_denied"
	FinishReasonUnknown          FinishReason = "unknown"
)

type ContentPart interface {
	isPart()
}

type ReasoningContent struct {
	Thinking   string `json:"thinking"`
	Signature  string `json:"signature"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

func (ReasoningContent) isPart() {}

type TextContent struct {
	Text string `json:"text"`
}

func (TextContent) isPart() {}

// ImageContent is an image attached to a message; only its name is shown.
type ImageContent struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
}

func (ImageContent) isPart() {}

type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Finished bool   `json:"finished"`
}

func (ToolCall) isPart() {}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	MIMEType   string `json:"mime_type"`
	Metadata   string `json:"metadata"`
	Status     string `json:"status"`
	IsError    bool   `json:"is_error"`
}

func (ToolResult) isPart() {}

type Finish struct {
	Reason  FinishReason `json:"reason"`
	Time    int64        `json:"time"`
	Message string       `json:"message,omitempty"`
	Details string       `json:"details,omitempty"`
}

func (Finish) isPart() {}

type Message struct {
	ID               string
	Role             MessageRole
	SessionID        string
	RunID            string
	Parts            []ContentPart
	Model            string
	Provider         string
	CreatedAt        int64
	UpdatedAt        int64
	IsSummaryMessage bool
	Boundary         *ContextBoundary
}

// ContextBoundary marks a message the model's context starts after: a
// compaction with its summary, or a /clear.
type ContextBoundary struct {
	Summary      string
	Cleared      bool
	TokensBefore int
	TokensAfter  int
}

func (m *Message) Content() TextContent {
	for _, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			return c
		}
	}
	return TextContent{}
}

func (m *Message) ReasoningContent() ReasoningContent {
	for _, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			return c
		}
	}
	return ReasoningContent{}
}

func (m *Message) Images() []ImageContent {
	items := make([]ImageContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ImageContent); ok {
			items = append(items, c)
		}
	}
	return items
}

func (m *Message) ToolCalls() []ToolCall {
	items := make([]ToolCall, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			items = append(items, c)
		}
	}
	return items
}

func (m *Message) ToolResults() []ToolResult {
	items := make([]ToolResult, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ToolResult); ok {
			items = append(items, c)
		}
	}
	return items
}

func (m *Message) IsFinished() bool {
	return m.FinishPart() != nil
}

func (m *Message) FinishPart() *Finish {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return &c
		}
	}
	return nil
}

func (m *Message) FinishReason() FinishReason {
	if finish := m.FinishPart(); finish != nil {
		return finish.Reason
	}
	return FinishReasonUnknown
}

func (m *Message) IsThinking() bool {
	return m.ReasoningContent().Thinking != "" && m.Content().Text == "" && !m.IsFinished()
}

func (m *Message) Clone() Message {
	clone := *m
	clone.Parts = make([]ContentPart, len(m.Parts))
	copy(clone.Parts, m.Parts)
	return clone
}

func (m *Message) AddFinish(reason FinishReason, message string, details string) {
	for i, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			m.Parts = slices.Delete(m.Parts, i, i+1)
			break
		}
	}
	m.Parts = append(m.Parts, Finish{
		Reason:  reason,
		Time:    time.Now().Unix(),
		Message: message,
		Details: details,
	})
}
