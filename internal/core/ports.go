package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type SessionStore interface {
	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, sessionID string) (Session, error)
	ListSessions(ctx context.Context, filter SessionListFilter) ([]Session, error)
	UpdateSession(ctx context.Context, session Session) error
	DeleteSession(ctx context.Context, sessionID string) error
}

type SubagentTaskStore interface {
	CreateSubagentTask(ctx context.Context, task SubagentTask) error
	UpdateSubagentTask(ctx context.Context, task SubagentTask) error
	GetSubagentTask(ctx context.Context, taskID string) (SubagentTask, error)
	GetSubagentTaskByParentToolCall(ctx context.Context, parentSessionID string, parentRunID string, parentToolCallID string) (SubagentTask, error)
	GetSubagentTaskByChildRun(ctx context.Context, childRunID string) (SubagentTask, error)
	ListSubagentTasks(ctx context.Context, filter SubagentTaskFilter) ([]SubagentTask, error)
	ListActiveSubagentTasksByParent(ctx context.Context, parentSessionID string) ([]SubagentTask, error)
	ListPendingSubagentCompletionTasks(ctx context.Context, limit int) ([]SubagentTask, error)
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
	SaveMessage(ctx context.Context, message transcript.Message) error
	AppendMessage(ctx context.Context, message transcript.Message) (int64, error)
	UpdateMessage(ctx context.Context, message transcript.Message) error
	GetMessage(ctx context.Context, messageID string) (transcript.Message, error)
	HasToolResult(ctx context.Context, sessionID string, toolCallID string) (bool, error)
	ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error)
	ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error)
	LatestCompaction(ctx context.Context, sessionID string) (transcript.Message, error)
}

type RunStore interface {
	CreateRun(ctx context.Context, run Run) error
	GetRun(ctx context.Context, runID string) (Run, error)
	GetActiveRunBySession(ctx context.Context, sessionID string) (Run, error)
	GetLatestRunBySession(ctx context.Context, sessionID string) (Run, error)
	ListActiveRuns(ctx context.Context) ([]Run, error)
	UpdateRun(ctx context.Context, run Run) error
	CompleteRun(ctx context.Context, assistantMessage transcript.Message, run Run) error

	AcceptMessage(ctx context.Context, message transcript.Message, run Run, deliveries ...ClientDelivery) error
}

type SessionInputStore interface {
	CreateSessionInput(ctx context.Context, input SessionInput) error
	UpdateSessionInput(ctx context.Context, input SessionInput) error
	ListPendingSessionInputs(ctx context.Context, sessionID string) ([]SessionInput, error)
	NextPendingSessionInput(ctx context.Context, sessionID string) (SessionInput, error)
	ListPendingSteerInputs(ctx context.Context, sessionID string, runID string) ([]SessionInput, error)
}

type UsageStore interface {
	SaveRunStep(ctx context.Context, step RunStep) error
	ListRunSteps(ctx context.Context, runID string) ([]RunStep, error)
	ListUsageRecords(ctx context.Context, filter UsageFilter) ([]UsageRecord, error)
}

type SessionBudgetStore interface {
	GetSessionBudget(ctx context.Context, sessionID string) (SessionBudget, error)
	SaveSessionBudget(ctx context.Context, sessionID string, budget SessionBudget, updatedAt time.Time) error
}

// EngineStateStore keeps the engine counters a session's next run starts from.
type EngineStateStore interface {
	GetSessionEngineState(ctx context.Context, sessionID string) (json.RawMessage, error)
	SaveSessionEngineState(ctx context.Context, sessionID string, state json.RawMessage, updatedAt time.Time) error
}

// TodoStore keeps each session's todo list.
type TodoStore interface {
	GetSessionTodo(ctx context.Context, sessionID string) (todo.List, error)
	SaveSessionTodo(ctx context.Context, list todo.List) error
}

type SearchStore interface {
	SearchMessages(ctx context.Context, filter SearchFilter) ([]SearchResult, error)
}

type MemoryStore interface {
	CreateMemory(ctx context.Context, entry MemoryEntry) error
	UpdateMemory(ctx context.Context, entry MemoryEntry) error
	DeleteMemory(ctx context.Context, id string) error
	GetMemory(ctx context.Context, id string) (MemoryEntry, error)
	ListMemories(ctx context.Context, filter MemoryFilter) ([]MemoryEntry, error)
}

type ApprovalStore interface {
	CreateApproval(ctx context.Context, approval Approval) error
	GetApproval(ctx context.Context, approvalID string) (Approval, error)
	UpdateApproval(ctx context.Context, approval Approval) error
	ListApprovals(ctx context.Context, sessionID string, state ApprovalState) ([]Approval, error)
}

// PermissionRuleStore keeps permission rules; global rules belong to no session.
type PermissionRuleStore interface {
	CreatePermissionRule(ctx context.Context, rule permission.Rule) error
	DeletePermissionRule(ctx context.Context, ruleID string) error
	ListPermissionRules(ctx context.Context, sessionIDs []string) ([]permission.Rule, error)
}

type FileSnapshotStore interface {
	CreateFileSnapshot(ctx context.Context, snapshot FileSnapshot) (FileSnapshot, error)
	ListFileSnapshots(ctx context.Context, sessionID string) ([]FileSnapshot, error)
}

type Store interface {
	SessionStore
	SubagentTaskStore
	TaskStore
	BindingStore
	DeliveryStore
	MessageStore
	RunStore
	SessionInputStore
	UsageStore
	SessionBudgetStore
	EngineStateStore
	TodoStore
	SearchStore
	MemoryStore
	ApprovalStore
	PermissionRuleStore
	FileSnapshotStore
}

type RunStarter interface {
	StartRun(ctx context.Context, runID string) error
}

type ToolExecutor interface {
	List() []tools.Spec
	Spec(toolID string) (tools.Spec, bool)
	Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error)
	// Subject is what permission rules match of a call.
	Subject(toolID string, call tools.Call) permission.Subject
	// ConcurrencyKey names what the call must not share with a concurrent call.
	ConcurrencyKey(toolID string, call tools.Call) string
}

type SessionLLMRegistry interface {
	ActiveSelection() (providerID string, modelID string)
	Providers() []SessionProviderOption
	Normalize(providerID string, modelID string) (SessionProviderOption, string, error)
	Models(ctx context.Context, providerID string) ([]string, error)
	Resolve(ctx context.Context, providerID string, modelID string) (providers.Runtime, SessionProviderOption, string, error)
}

type SessionLLMContextWindowRegistry interface {
	ContextWindowTokens(providerID string, modelID string) (int, bool)
}

// TaskStore keeps the background tasks of sessions. FinishTask ends a task
// once and reports whether this call ended it.
type TaskStore interface {
	CreateTask(ctx context.Context, task Task) error
	GetTask(ctx context.Context, taskID string) (Task, error)
	ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error)
	FinishTask(ctx context.Context, taskID string, status TaskStatus, exitCode *int, errText string, at time.Time) (bool, error)
	SetTaskCursor(ctx context.Context, taskID string, cursor int64) error
	MarkTasksDelivered(ctx context.Context, taskIDs []string, runID string, at time.Time) error
}
