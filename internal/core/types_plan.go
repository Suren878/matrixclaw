package core

import "time"

type PlanItemStatus string

const (
	PlanItemPending PlanItemStatus = "pending"
	PlanItemActive  PlanItemStatus = "active"
	PlanItemDone    PlanItemStatus = "done"
	PlanItemSkipped PlanItemStatus = "skipped"
)

type SessionPlan struct {
	SessionID string     `json:"session_id"`
	Goal      string     `json:"goal,omitempty"`
	Items     []PlanItem `json:"items,omitempty"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
}

type PlanItem struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Text      string         `json:"text"`
	Status    PlanItemStatus `json:"status"`
	Position  int            `json:"position"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type PlanRunStatus string

const (
	PlanRunIdle      PlanRunStatus = "idle"
	PlanRunRunning   PlanRunStatus = "running"
	PlanRunBlocked   PlanRunStatus = "blocked"
	PlanRunFailed    PlanRunStatus = "failed"
	PlanRunCompleted PlanRunStatus = "completed"
	PlanRunCanceled  PlanRunStatus = "canceled"
)

type PlanRun struct {
	SessionID     string        `json:"session_id"`
	Status        PlanRunStatus `json:"status"`
	CurrentItemID string        `json:"current_item_id,omitempty"`
	LastRunID     string        `json:"last_run_id,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	StepNo        int           `json:"step_no,omitempty"`
	Attempt       int           `json:"attempt,omitempty"`
	CreatedAt     time.Time     `json:"created_at,omitempty"`
	UpdatedAt     time.Time     `json:"updated_at,omitempty"`
}
