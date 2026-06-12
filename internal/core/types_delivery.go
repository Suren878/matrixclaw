package core

import (
	"encoding/json"
	"time"
)

type ClientBinding struct {
	Client      string    `json:"client"`
	ExternalKey string    `json:"external_key"`
	SessionID   string    `json:"session_id"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ClientDeliveryStatus string

const (
	ClientDeliveryStatusPending ClientDeliveryStatus = "pending"
	ClientDeliveryStatusReady   ClientDeliveryStatus = "ready"
	ClientDeliveryStatusSent    ClientDeliveryStatus = "sent"
	ClientDeliveryStatusFailed  ClientDeliveryStatus = "failed"
)

type ClientDeliveryTarget struct {
	Client      string          `json:"client,omitempty"`
	ExternalKey string          `json:"external_key,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	RunID       string          `json:"run_id,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Address     json.RawMessage `json:"address,omitempty"`
}

type ClientDelivery struct {
	ID          string               `json:"id"`
	Type        string               `json:"type"`
	Client      string               `json:"client"`
	ExternalKey string               `json:"external_key,omitempty"`
	SessionID   string               `json:"session_id,omitempty"`
	RunID       string               `json:"run_id,omitempty"`
	TaskID      string               `json:"task_id,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Address     json.RawMessage      `json:"address,omitempty"`
	Payload     json.RawMessage      `json:"payload,omitempty"`
	Status      ClientDeliveryStatus `json:"status"`
	Error       string               `json:"error,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	FinishedAt  *time.Time           `json:"finished_at,omitempty"`
}

type ClientDeliveryFilter struct {
	Client       string
	ExternalKey  string
	SessionID    string
	RunID        string
	TaskID       string
	Type         string
	Status       ClientDeliveryStatus
	CreatedAfter time.Time
	Limit        int
}

type UseBindingInput struct {
	Client      string `json:"client"`
	ExternalKey string `json:"external_key"`
	SessionID   string `json:"session_id"`
}
