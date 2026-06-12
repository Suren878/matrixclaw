package core

import (
	"encoding/json"
	"time"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

type Message struct {
	ID        string        `json:"id"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Role      MessageRole   `json:"role"`
	Content   string        `json:"content"`
	Parts     []MessagePart `json:"parts,omitempty"`
	Model     string        `json:"model,omitempty"`
	Provider  string        `json:"provider,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type MessagePartKind string

const (
	MessagePartKindText       MessagePartKind = "text"
	MessagePartKindImage      MessagePartKind = "image"
	MessagePartKindReasoning  MessagePartKind = "reasoning"
	MessagePartKindToolCall   MessagePartKind = "tool_call"
	MessagePartKindToolResult MessagePartKind = "tool_result"
	MessagePartKindFinish     MessagePartKind = "finish"
)

type MessagePart struct {
	Kind       MessagePartKind `json:"kind"`
	Text       *TextPart       `json:"text,omitempty"`
	Image      *ImagePart      `json:"image,omitempty"`
	Reasoning  *ReasoningPart  `json:"reasoning,omitempty"`
	ToolCall   *ToolCallPart   `json:"tool_call,omitempty"`
	ToolResult *ToolResultPart `json:"tool_result,omitempty"`
	Finish     *FinishPart     `json:"finish,omitempty"`
}

type TextPart struct {
	Text string `json:"text"`
}

type ImagePart struct {
	MIMEType    string `json:"mime_type,omitempty"`
	DataBase64  string `json:"data_base64,omitempty"`
	Name        string `json:"name,omitempty"`
	StoragePath string `json:"storage_path,omitempty"`
	Temporary   bool   `json:"temporary,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

type ReasoningPart struct {
	Text             string          `json:"text"`
	Signature        string          `json:"signature,omitempty"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
	ToolID           string          `json:"tool_id,omitempty"`
	ResponsesData    json.RawMessage `json:"responses_data,omitempty"`
}

type ToolCallPart struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Finished bool   `json:"finished,omitempty"`
}

type ToolResultPart struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Content    string          `json:"content"`
	MIMEType   string          `json:"mime_type,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Status     string          `json:"status,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
}

type FinishPart struct {
	Reason  string          `json:"reason,omitempty"`
	Message string          `json:"message,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
}

func NormalizeMessageParts(content string, parts []MessagePart) []MessagePart {
	if len(parts) > 0 {
		return parts
	}
	if content == "" {
		return nil
	}
	return []MessagePart{{
		Kind: MessagePartKindText,
		Text: &TextPart{Text: content},
	}}
}
