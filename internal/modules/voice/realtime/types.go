package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	ModuleID        = "realtime_voice"
	ProviderGemini  = "gemini_live"
	ProviderGrok    = "grok_voice"
	ProtocolVersion = 1
)

var (
	ErrDisabled            = errors.New("realtime voice is disabled")
	ErrProviderUnavailable = errors.New("realtime voice provider is unavailable")
	ErrInvalidRequest      = errors.New("invalid realtime voice request")
	ErrSessionNotFound     = errors.New("realtime voice session not found")
)

type Config struct {
	Enabled     bool
	ProviderID  string
	MaxSessions int
	PersistMode PersistMode
}

type AudioFormat struct {
	Encoding     string `json:"encoding"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
}

type PersistMode string

const (
	PersistModeNone            PersistMode = "none"
	PersistModeTurnsAndSummary PersistMode = "turns_and_summary"
)

type SessionCreateRequest struct {
	Client            string      `json:"client,omitempty"`
	ExternalKey       string      `json:"external_key,omitempty"`
	SessionID         string      `json:"session_id,omitempty"`
	WorkingDir        string      `json:"working_dir,omitempty"`
	ProviderID        string      `json:"provider_id,omitempty"`
	ModelID           string      `json:"model_id,omitempty"`
	VoiceID           string      `json:"voice_id,omitempty"`
	Language          string      `json:"language,omitempty"`
	SystemInstruction string      `json:"system_instruction,omitempty"`
	InputAudio        AudioFormat `json:"input_audio,omitempty"`
	OutputAudio       AudioFormat `json:"output_audio,omitempty"`
	PersistMode       PersistMode `json:"persist_mode,omitempty"`
}

type SessionCreateResponse struct {
	Session SessionInfo `json:"session"`
}

type SessionResponse struct {
	Session SessionInfo `json:"session"`
}

type ModuleResponse struct {
	Module ModuleDescriptor `json:"module"`
}

type ModuleDescriptor struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	Enabled      bool                  `json:"enabled"`
	ProviderID   string                `json:"provider_id"`
	ProviderName string                `json:"provider_name,omitempty"`
	ModelID      string                `json:"model_id,omitempty"`
	Status       string                `json:"status"`
	Config       ProviderConfigSummary `json:"config"`
	InputAudio   AudioFormat           `json:"input_audio"`
	OutputAudio  AudioFormat           `json:"output_audio"`
	Providers    []ProviderDescriptor  `json:"providers"`
}

type ProviderConfigSummary struct {
	APIKeyConfigured bool   `json:"api_key_configured"`
	APIKeyValid      bool   `json:"api_key_valid,omitempty"`
	APIKeyPreview    string `json:"api_key_preview,omitempty"`
	APIKeyError      string `json:"api_key_error,omitempty"`
	APIKeyEnv        string `json:"api_key_env,omitempty"`
	ModelID          string `json:"model_id,omitempty"`
	VoiceID          string `json:"voice_id,omitempty"`
	Language         string `json:"language,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
}

type ProviderDescriptor struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Status        string                `json:"status,omitempty"`
	Configured    bool                  `json:"configured"`
	Config        ProviderConfigSummary `json:"config"`
	DefaultModel  string                `json:"default_model,omitempty"`
	Models        []string              `json:"models,omitempty"`
	Voices        []string              `json:"voices,omitempty"`
	InputFormats  []AudioFormat         `json:"input_formats,omitempty"`
	OutputFormats []AudioFormat         `json:"output_formats,omitempty"`
}

type SessionStatus string

const (
	SessionStatusCreated   SessionStatus = "created"
	SessionStatusStreaming SessionStatus = "streaming"
	SessionStatusClosed    SessionStatus = "closed"
	SessionStatusFailed    SessionStatus = "failed"
)

type SessionInfo struct {
	ID            string        `json:"id"`
	Status        SessionStatus `json:"status"`
	ProviderID    string        `json:"provider_id"`
	ProviderName  string        `json:"provider_name,omitempty"`
	ModelID       string        `json:"model_id,omitempty"`
	VoiceID       string        `json:"voice_id,omitempty"`
	Language      string        `json:"language,omitempty"`
	CoreSessionID string        `json:"session_id"`
	Client        string        `json:"client,omitempty"`
	ExternalKey   string        `json:"external_key,omitempty"`
	PersistMode   PersistMode   `json:"persist_mode"`
	InputAudio    AudioFormat   `json:"input_audio"`
	OutputAudio   AudioFormat   `json:"output_audio"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	ClosedAt      *time.Time    `json:"closed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type EventType string

const (
	EventInputAudioAppend         EventType = "input_audio.append"
	EventInputAudioEnd            EventType = "input_audio.end"
	EventInputTextAppend          EventType = "input_text.append"
	EventResponseCancel           EventType = "response.cancel"
	EventSessionClose             EventType = "session.close"
	EventSessionReady             EventType = "session.ready"
	EventInputTranscriptDelta     EventType = "input_transcript.delta"
	EventInputTranscriptFinal     EventType = "input_transcript.final"
	EventAssistantTranscriptDelta EventType = "assistant_transcript.delta"
	EventAssistantTranscriptFinal EventType = "assistant_transcript.final"
	EventAssistantAudioDelta      EventType = "assistant_audio.delta"
	EventInterrupted              EventType = "interrupted"
	EventTurnFinal                EventType = "turn.final"
	EventToolCall                 EventType = "tool.call"
	EventToolResult               EventType = "tool.result"
	EventApprovalRequested        EventType = "approval.requested"
	EventApprovalResolved         EventType = "approval.resolved"
	EventBackpressure             EventType = "backpressure"
	EventError                    EventType = "error"
	EventSessionClosed            EventType = "session.closed"
)

type Event struct {
	V              int             `json:"v"`
	Type           EventType       `json:"type"`
	Seq            int64           `json:"seq,omitempty"`
	VoiceSessionID string          `json:"voice_session_id,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	At             time.Time       `json:"at,omitempty"`
}

type InputAudioPayload struct {
	AudioBase64 string `json:"audio_base64"`
	DurationMs  int    `json:"duration_ms,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

type InputTextPayload struct {
	Text      string `json:"text"`
	EndOfTurn bool   `json:"end_of_turn,omitempty"`
}

type TranscriptPayload struct {
	Text  string `json:"text"`
	Final bool   `json:"final,omitempty"`
}

type AssistantAudioPayload struct {
	AudioBase64 string `json:"audio_base64"`
	MIMEType    string `json:"mime_type,omitempty"`
	DurationMs  int    `json:"duration_ms,omitempty"`
}

type TurnFinalPayload struct {
	InputTranscript     string `json:"input_transcript,omitempty"`
	AssistantTranscript string `json:"assistant_transcript,omitempty"`
}

type ToolCallPayload struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ToolResultPayload struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

type ApprovalRequestedPayload struct {
	ID          string `json:"id"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	ToolName    string `json:"tool_name"`
	Action      string `json:"action,omitempty"`
	Path        string `json:"path,omitempty"`
	Description string `json:"description,omitempty"`
}

type ApprovalResolvedPayload struct {
	ID         string `json:"id"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Granted    bool   `json:"granted"`
	Denied     bool   `json:"denied"`
}

type ErrorPayload struct {
	Code        string `json:"code,omitempty"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable,omitempty"`
}

type Stream interface {
	Read(context.Context) (Event, error)
	Write(context.Context, Event) error
	Close(error) error
}

type CoreBridge interface {
	CreateSession(context.Context, core.CreateSessionInput) (core.Session, error)
	GetSession(context.Context, string) (core.Session, error)
	CurrentBinding(context.Context, string, string) (core.ClientBinding, error)
	UseBinding(context.Context, core.UseBindingInput) (core.ClientBinding, error)
	ListToolSpecs() []tools.Spec
	ExecuteTool(context.Context, core.ExecuteToolInput) (core.ExecuteToolResult, error)
	CommitRealtimeVoiceTurn(context.Context, core.CommitRealtimeVoiceTurnInput) (core.CommitRealtimeVoiceTurnResult, error)
	SubscribeEvents(context.Context, string) <-chan core.Event
}

type Provider interface {
	Descriptor(context.Context) ProviderDescriptor
	Connect(context.Context, ProviderConnectRequest) (ProviderConnection, error)
}

type ProviderConnectRequest struct {
	VoiceSessionID    string
	SessionID         string
	Client            string
	WorkingDir        string
	ModelID           string
	VoiceID           string
	Language          string
	SystemInstruction string
	InputAudio        AudioFormat
	OutputAudio       AudioFormat
	Tools             []ToolDeclaration
}

type ToolDeclaration struct {
	Name             string
	Description      string
	Parameters       json.RawMessage
	RequiresApproval bool
}

type ProviderConnection interface {
	Send(context.Context, ProviderInput) error
	Receive(context.Context) (ProviderOutput, error)
	Close(error) error
}

type ProviderInputType string

const (
	ProviderInputAudioAppend ProviderInputType = "audio_append"
	ProviderInputAudioEnd    ProviderInputType = "audio_end"
	ProviderInputTextAppend  ProviderInputType = "text_append"
	ProviderInputCancel      ProviderInputType = "cancel"
	ProviderInputToolResult  ProviderInputType = "tool_result"
)

type ProviderInput struct {
	Type          ProviderInputType
	AudioBase64   string
	AudioMIMEType string
	Text          string
	EndOfTurn     bool
	ToolResponses []ProviderToolResponse
}

type ProviderToolResponse struct {
	ID       string
	Name     string
	Response map[string]any
}

type ProviderOutputType string

const (
	ProviderOutputInputTranscript     ProviderOutputType = "input_transcript"
	ProviderOutputAssistantTranscript ProviderOutputType = "assistant_transcript"
	ProviderOutputAssistantAudio      ProviderOutputType = "assistant_audio"
	ProviderOutputTurnComplete        ProviderOutputType = "turn_complete"
	ProviderOutputInterrupted         ProviderOutputType = "interrupted"
	ProviderOutputToolCall            ProviderOutputType = "tool_call"
	ProviderOutputGoAway              ProviderOutputType = "go_away"
	ProviderOutputSessionResumption   ProviderOutputType = "session_resumption"
	ProviderOutputError               ProviderOutputType = "error"
)

type ProviderOutput struct {
	Type         ProviderOutputType
	Text         string
	AudioBase64  string
	MIMEType     string
	ToolCalls    []ProviderToolCall
	Resumable    bool
	ResumeHandle string
	Error        string
	Raw          json.RawMessage
}

type ProviderToolCall struct {
	ID   string
	Name string
	Args json.RawMessage
}
