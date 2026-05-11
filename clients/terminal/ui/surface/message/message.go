package message

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
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
	Thinking         string          `json:"thinking"`
	Signature        string          `json:"signature"`
	ThoughtSignature string          `json:"thought_signature"`
	ToolID           string          `json:"tool_id"`
	ResponsesData    json.RawMessage `json:"responses_data,omitempty"`
	StartedAt        int64           `json:"started_at,omitempty"`
	FinishedAt       int64           `json:"finished_at,omitempty"`
}

func (ReasoningContent) isPart() {}

type TextContent struct {
	Text string `json:"text"`
}

func (TextContent) isPart() {}

type ImageURLContent struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func (ImageURLContent) isPart() {}

type BinaryContent struct {
	Path     string `json:"path,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

func (BinaryContent) isPart() {}

func (bc BinaryContent) String() string {
	return base64.StdEncoding.EncodeToString(bc.Data)
}

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
	Parts            []ContentPart
	Model            string
	Provider         string
	CreatedAt        int64
	UpdatedAt        int64
	IsSummaryMessage bool
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

func (m *Message) ImageURLContent() []ImageURLContent {
	items := make([]ImageURLContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ImageURLContent); ok {
			items = append(items, c)
		}
	}
	return items
}

func (m *Message) BinaryContent() []BinaryContent {
	items := make([]BinaryContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(BinaryContent); ok {
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

func (m *Message) AppendContent(delta string) {
	for i, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			m.Parts[i] = TextContent{Text: c.Text + delta}
			return
		}
	}
	m.Parts = append(m.Parts, TextContent{Text: delta})
}

func (m *Message) AppendReasoningContent(delta string) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			m.Parts[i] = ReasoningContent{
				Thinking:         c.Thinking + delta,
				Signature:        c.Signature,
				ThoughtSignature: c.ThoughtSignature,
				ToolID:           c.ToolID,
				ResponsesData:    append(json.RawMessage(nil), c.ResponsesData...),
				StartedAt:        c.StartedAt,
				FinishedAt:       c.FinishedAt,
			}
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{
		Thinking:  delta,
		StartedAt: time.Now().Unix(),
	})
}

func (m *Message) AppendThoughtSignature(signature string, toolCallID string) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			m.Parts[i] = ReasoningContent{
				Thinking:         c.Thinking,
				Signature:        c.Signature,
				ThoughtSignature: c.ThoughtSignature + signature,
				ToolID:           toolCallID,
				ResponsesData:    append(json.RawMessage(nil), c.ResponsesData...),
				StartedAt:        c.StartedAt,
				FinishedAt:       c.FinishedAt,
			}
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{ThoughtSignature: signature, ToolID: toolCallID})
}

func (m *Message) AppendReasoningSignature(signature string) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			m.Parts[i] = ReasoningContent{
				Thinking:         c.Thinking,
				Signature:        c.Signature + signature,
				ThoughtSignature: c.ThoughtSignature,
				ToolID:           c.ToolID,
				ResponsesData:    append(json.RawMessage(nil), c.ResponsesData...),
				StartedAt:        c.StartedAt,
				FinishedAt:       c.FinishedAt,
			}
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{Signature: signature})
}

func (m *Message) SetReasoningResponsesData(data json.RawMessage) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			c.ResponsesData = append(json.RawMessage(nil), data...)
			m.Parts[i] = c
			return
		}
	}
	if len(data) > 0 {
		m.Parts = append(m.Parts, ReasoningContent{ResponsesData: append(json.RawMessage(nil), data...)})
	}
}

func (m *Message) FinishThinking() {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			if c.FinishedAt == 0 {
				c.FinishedAt = time.Now().Unix()
				m.Parts[i] = c
			}
			return
		}
	}
}

func (m *Message) ThinkingDuration() time.Duration {
	reasoning := m.ReasoningContent()
	if reasoning.StartedAt == 0 {
		return 0
	}
	end := reasoning.FinishedAt
	if end == 0 {
		end = time.Now().Unix()
	}
	return time.Duration(end-reasoning.StartedAt) * time.Second
}

func (m *Message) FinishToolCall(toolCallID string) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && c.ID == toolCallID {
			c.Finished = true
			m.Parts[i] = c
			return
		}
	}
}

func (m *Message) AppendToolCallInput(toolCallID string, inputDelta string) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && c.ID == toolCallID {
			c.Input += inputDelta
			m.Parts[i] = c
			return
		}
	}
}

func (m *Message) AddToolCall(tc ToolCall) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && c.ID == tc.ID {
			m.Parts[i] = tc
			return
		}
	}
	m.Parts = append(m.Parts, tc)
}

func (m *Message) SetToolCalls(tc []ToolCall) {
	parts := make([]ContentPart, 0, len(m.Parts)+len(tc))
	for _, part := range m.Parts {
		if _, ok := part.(ToolCall); ok {
			continue
		}
		parts = append(parts, part)
	}
	for _, item := range tc {
		parts = append(parts, item)
	}
	m.Parts = parts
}

func (m *Message) AddToolResult(tr ToolResult) {
	m.Parts = append(m.Parts, tr)
}

func (m *Message) SetToolResults(tr []ToolResult) {
	for _, item := range tr {
		m.Parts = append(m.Parts, item)
	}
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

func (m *Message) AddImageURL(url string, detail string) {
	m.Parts = append(m.Parts, ImageURLContent{URL: url, Detail: detail})
}

func (m *Message) AddBinary(mimeType string, data []byte) {
	m.Parts = append(m.Parts, BinaryContent{MIMEType: mimeType, Data: data})
}

func (m *Message) HasRenderableText() bool {
	return strings.TrimSpace(m.Content().Text) != "" || strings.TrimSpace(m.ReasoningContent().Thinking) != ""
}
