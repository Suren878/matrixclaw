package transcript

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
	ID         string        `json:"id"`
	Seq        int64         `json:"seq,omitempty"`
	SessionID  string        `json:"session_id"`
	RunID      string        `json:"run_id"`
	Role       MessageRole   `json:"role"`
	Origin     Origin        `json:"origin,omitempty"`
	Content    string        `json:"content"`
	Parts      []MessagePart `json:"parts,omitempty"`
	Compaction *Compaction   `json:"compaction,omitempty"`
	Model      string        `json:"model,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// Compaction makes a message a context boundary: the model sees Summary and Kept
// instead of the session's messages up to CoversThroughSeq. Cleared marks a
// /clear, which keeps nothing. Tokens are estimates of the prompt around it.
type Compaction struct {
	Summary          string   `json:"summary,omitempty"`
	Kept             []string `json:"kept,omitempty"`
	CoversThroughSeq int64    `json:"covers_through_seq"`
	RunID            string   `json:"run_id,omitempty"`
	TokensBefore     int      `json:"tokens_before,omitempty"`
	TokensAfter      int      `json:"tokens_after,omitempty"`
	Cleared          bool     `json:"cleared,omitempty"`
}

// Origin says who wrote a message when its role alone does not.
type Origin string

// Engine notes reach the model as user text. Clients show OriginEngine notes as
// system notes and hide OriginEngineModel ones, which only the model needs.
const (
	OriginEngine      Origin = "engine"
	OriginEngineModel Origin = "engine_model"
)

// IsEngine reports whether the message is an engine note of either kind.
func (o Origin) IsEngine() bool {
	return o == OriginEngine || o == OriginEngineModel
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
	Text         string `json:"text"`
	Signature    string `json:"signature,omitempty"`
	RedactedData string `json:"redacted_data,omitempty"`
}

type ToolCallPart struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Finished bool   `json:"finished,omitempty"`
	// Deferred marks a call held back behind an approval barrier; it has not started.
	Deferred bool `json:"deferred,omitempty"`
}

type ToolResultPart struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Content    string          `json:"content"`
	MIMEType   string          `json:"mime_type,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	// Status is success, error or neutral; empty is success.
	Status string `json:"status,omitempty"`
	// Guidance is user steering delivered with this result; Content carries it too.
	Guidance []string `json:"guidance,omitempty"`
	// OutputPath is the file holding the full output when Content was cut.
	OutputPath string `json:"output_path,omitempty"`
}

// IsError reports whether the call failed.
func (p ToolResultPart) IsError() bool {
	return p.Status == "error"
}

// UnmarshalJSON reads a part written before status was the only status field:
// is_error without a status is an error.
func (p *ToolResultPart) UnmarshalJSON(data []byte) error {
	type plain ToolResultPart
	var legacy struct {
		plain
		IsError bool `json:"is_error"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	*p = ToolResultPart(legacy.plain)
	if p.Status == "" && legacy.IsError {
		p.Status = "error"
	}
	return nil
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
