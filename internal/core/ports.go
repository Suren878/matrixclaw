package core

import (
	"context"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type SessionStore interface {
	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, sessionID string) (Session, error)
	ListSessions(ctx context.Context, filter SessionListFilter) ([]Session, error)
	UpdateSession(ctx context.Context, session Session) error
	DeleteSession(ctx context.Context, sessionID string) error
}

type BindingStore interface {
	SaveBinding(ctx context.Context, binding ClientBinding) error
	GetBinding(ctx context.Context, client string, externalKey string) (ClientBinding, error)
}

type DeliveryStore interface {
	CreateClientDelivery(ctx context.Context, delivery ClientDelivery) error
	ListClientDeliveries(ctx context.Context, filter ClientDeliveryFilter) ([]ClientDelivery, error)
	UpdateClientDelivery(ctx context.Context, delivery ClientDelivery) error
}

type MessageStore interface {
	SaveMessage(ctx context.Context, message Message) error
	UpdateMessage(ctx context.Context, message Message) error
	ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error)
}

type RunStore interface {
	CreateRun(ctx context.Context, run Run) error
	GetRun(ctx context.Context, runID string) (Run, error)
	UpdateRun(ctx context.Context, run Run) error
	CompleteRun(ctx context.Context, assistantMessage Message, run Run) error

	AcceptMessage(ctx context.Context, message Message, run Run) error
}

type UsageStore interface {
	SaveUsageRecord(ctx context.Context, record UsageRecord) error
	ListUsageRecords(ctx context.Context, filter UsageFilter) ([]UsageRecord, error)
}

type PlanStore interface {
	GetSessionPlan(ctx context.Context, sessionID string) (SessionPlan, error)
	SetSessionGoal(ctx context.Context, sessionID string, goal string, updatedAt time.Time) error
	ClearSessionPlan(ctx context.Context, sessionID string) error
	AddPlanItem(ctx context.Context, item PlanItem) error
	UpdatePlanItem(ctx context.Context, item PlanItem) error
	GetPlanItem(ctx context.Context, itemID string) (PlanItem, error)
	NextPlanItemPosition(ctx context.Context, sessionID string, parentID string) (int, error)
	GetPlanRun(ctx context.Context, sessionID string) (PlanRun, error)
	SavePlanRun(ctx context.Context, run PlanRun) error
	ClearPlanRun(ctx context.Context, sessionID string) error
}

type SearchStore interface {
	SearchMessages(ctx context.Context, filter SearchFilter) ([]SearchResult, error)
}

type ApprovalStore interface {
	CreateApproval(ctx context.Context, approval Approval) error
	GetApproval(ctx context.Context, approvalID string) (Approval, error)
	UpdateApproval(ctx context.Context, approval Approval) error
	ListApprovals(ctx context.Context, sessionID string, state ApprovalState) ([]Approval, error)
}

type FileSnapshotStore interface {
	CreateFileSnapshot(ctx context.Context, snapshot FileSnapshot) (FileSnapshot, error)
	ListFileSnapshots(ctx context.Context, sessionID string) ([]FileSnapshot, error)
}

type Store interface {
	SessionStore
	BindingStore
	DeliveryStore
	MessageStore
	RunStore
	UsageStore
	PlanStore
	SearchStore
	ApprovalStore
	FileSnapshotStore
}

type RunStarter interface {
	StartRun(ctx context.Context, runID string) error
}

type ToolExecutor interface {
	List() []tools.Spec
	Spec(toolID string) (tools.Spec, bool)
	Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error)
}

type SessionLLMRegistry interface {
	ActiveSelection() (providerID string, modelID string)
	Providers() []SessionProviderOption
	Normalize(providerID string, modelID string) (SessionProviderOption, string, error)
	Models(ctx context.Context, providerID string) ([]string, error)
	Resolve(ctx context.Context, providerID string, modelID string) (providers.Runtime, SessionProviderOption, string, error)
}
