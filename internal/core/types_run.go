package core

import (
	"encoding/json"
	"time"
)

type RunStatus string

const (
	RunStatusAccepted        RunStatus = "accepted"
	RunStatusRunning         RunStatus = "running"
	RunStatusWaitingApproval RunStatus = "waiting_approval"
	RunStatusCompleted       RunStatus = "completed"
	RunStatusCanceled        RunStatus = "canceled"
	RunStatusFailed          RunStatus = "failed"
)

type RunTiming struct {
	TotalMillis    int64     `json:"total_ms,omitempty"`
	ModelMillis    int64     `json:"model_ms,omitempty"`
	ToolMillis     int64     `json:"tool_ms,omitempty"`
	ApprovalMillis int64     `json:"approval_ms,omitempty"`
	LastEventAt    time.Time `json:"last_event_at,omitempty"`
}

type Run struct {
	ID                 string             `json:"id"`
	SessionID          string             `json:"session_id"`
	UserMessageID      string             `json:"user_message_id"`
	Client             string             `json:"client,omitempty"`
	ExternalKey        string             `json:"external_key,omitempty"`
	ClientCapabilities ClientCapabilities `json:"client_capabilities,omitempty"`
	Status             RunStatus          `json:"status"`
	Error              string             `json:"error,omitempty"`
	StartedAt          time.Time          `json:"started_at"`
	FinishedAt         *time.Time         `json:"finished_at,omitempty"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

type BusyInputMode string

const (
	BusyInputModeQueue     BusyInputMode = "queue"
	BusyInputModeSteer     BusyInputMode = "steer"
	BusyInputModeInterrupt BusyInputMode = "interrupt"
)

type SessionInputStatus string

const (
	SessionInputStatusPending  SessionInputStatus = "pending"
	SessionInputStatusConsumed SessionInputStatus = "consumed"
	SessionInputStatusError    SessionInputStatus = "error"
)

type SessionInput struct {
	ID                 string             `json:"id"`
	SessionID          string             `json:"session_id"`
	TargetRunID        string             `json:"target_run_id,omitempty"`
	Mode               BusyInputMode      `json:"mode"`
	Status             SessionInputStatus `json:"status"`
	Text               string             `json:"text"`
	Parts              []MessagePart      `json:"parts,omitempty"`
	Client             string             `json:"client,omitempty"`
	ExternalKey        string             `json:"external_key,omitempty"`
	ClientCapabilities ClientCapabilities `json:"client_capabilities,omitempty"`
	DeliveryAddress    json.RawMessage    `json:"delivery_address,omitempty"`
	WorkingDir         string             `json:"working_dir,omitempty"`
	ConsumedRunID      string             `json:"consumed_run_id,omitempty"`
	Error              string             `json:"error,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	ConsumedAt         *time.Time         `json:"consumed_at,omitempty"`
}

type HandleMessageInput struct {
	Client             string             `json:"client"`
	ExternalKey        string             `json:"external_key"`
	ClientCapabilities ClientCapabilities `json:"client_capabilities,omitempty"`
	SessionID          string             `json:"session_id"`
	Text               string             `json:"text"`
	Parts              []MessagePart      `json:"parts,omitempty"`
	BusyMode           BusyInputMode      `json:"busy_mode,omitempty"`
	WorkingDir         string             `json:"working_dir"`
	DeliveryAddress    json.RawMessage    `json:"delivery_address,omitempty"`
	AllowAutoBindOne   bool               `json:"allow_auto_bind_one"`
}

type HandleTriggeredRunInput struct {
	TriggerID          string
	Client             string
	ExternalKey        string
	ClientCapabilities ClientCapabilities
	SessionID          string
	Text               string
	WorkingDir         string
}

type ClientCapabilities struct {
	SupportsVoiceDelivery    bool `json:"supports_voice_delivery,omitempty"`
	SupportsDocumentDelivery bool `json:"supports_document_delivery,omitempty"`
}

type AcceptRunResult struct {
	SessionID   string          `json:"session_id"`
	Status      AcceptRunStatus `json:"status,omitempty"`
	UserMessage Message         `json:"user_message"`
	Run         Run             `json:"run"`
	Input       *SessionInput   `json:"input,omitempty"`
}

type AcceptRunStatus string

const (
	AcceptRunStatusStarted      AcceptRunStatus = "started"
	AcceptRunStatusQueued       AcceptRunStatus = "queued"
	AcceptRunStatusSteered      AcceptRunStatus = "steered"
	AcceptRunStatusInterrupting AcceptRunStatus = "interrupting"
)
