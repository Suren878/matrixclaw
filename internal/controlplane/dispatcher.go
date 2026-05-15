package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type ClientRuntime interface {
	ClientName() string
}

type SessionRuntime interface {
	CurrentBinding(ctx context.Context, externalKey string) (core.ClientBinding, error)
	ListSessions(ctx context.Context) ([]core.Session, error)
	CreateSession(ctx context.Context, externalKey string, title string, workingDir string) (core.Session, error)
	UseSession(ctx context.Context, externalKey string, sessionID string) (core.ClientBinding, error)
	RenameSession(ctx context.Context, sessionID string, title string) (core.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

type SessionRuntimeOptions interface {
	CreateSessionWithOptions(ctx context.Context, externalKey string, input core.CreateSessionRequest) (core.Session, error)
}

type ExternalAgentRuntime interface {
	ListExternalAgents(ctx context.Context) ([]core.ExternalAgentDescriptor, error)
	UpdateExternalAgent(ctx context.Context, agentID string, update core.UpdateExternalAgentRequest) ([]core.ExternalAgentDescriptor, error)
}

type ProviderRuntime interface {
	ListSetupProviders(ctx context.Context) ([]setup.ProviderSetupItem, error)
	ConfigureSetupProvider(ctx context.Context, providerID string, update setup.ProviderSetupUpdate) (setup.ProviderSetupItem, error)
	ProviderModels(ctx context.Context, providerID string, update setup.ProviderSetupUpdate) ([]string, error)
	DeleteSetupProvider(ctx context.Context, providerID string) error
	UpdateSessionProvider(ctx context.Context, sessionID string, providerID string) (core.Session, error)
}

type PermissionRuntime interface {
	UpdateSessionPermissionMode(ctx context.Context, sessionID string, mode core.PermissionMode) (core.Session, error)
}

type SessionMessageRuntime interface {
	CreateSystemMessage(ctx context.Context, sessionID string, content string) (core.Message, error)
}

type ContextRuntime interface {
	SessionContext(ctx context.Context, sessionID string) (core.ContextReport, error)
	CompactSession(ctx context.Context, sessionID string) (core.CompactSessionResult, error)
}

type UsageRuntime interface {
	SessionUsage(ctx context.Context, sessionID string) (core.UsageReport, error)
}

type PlanRuntime interface {
	SessionPlan(ctx context.Context, sessionID string) (core.SessionPlan, error)
	SetSessionGoal(ctx context.Context, sessionID string, goal string) (core.SessionPlan, error)
	ClearSessionPlan(ctx context.Context, sessionID string) (core.SessionPlan, error)
	AddPlanItem(ctx context.Context, sessionID string, text string, parentID string) (core.SessionPlan, error)
	UpdatePlanItem(ctx context.Context, sessionID string, itemID string, status core.PlanItemStatus, text string) (core.SessionPlan, error)
}

type SearchRuntime interface {
	Search(ctx context.Context, filter core.SearchFilter) (core.SearchReport, error)
}

type StorageRuntime interface {
	SaveStorageFile(ctx context.Context, storagePath string, content []byte, title string, tags []string, mimeType string) (localstorage.Entry, error)
	ListTemporaryStorageFiles(ctx context.Context, limit int) (localstorage.TempListResult, error)
	PromoteTemporaryStorageFile(ctx context.Context, tempPath string, destPath string) (localstorage.Entry, error)
	DeleteTemporaryStorageFile(ctx context.Context, tempPath string) (localstorage.TempEntry, error)
	CleanupTemporaryStorageFiles(ctx context.Context) (localstorage.CleanupResult, error)
	UpdateTemporaryStorageSettings(ctx context.Context, autoCleanup *bool, ttlDays int64, maxGB float64) (localstorage.TempSettings, error)
	ListStorageFiles(ctx context.Context, filter localstorage.ListFilter) (localstorage.ListResult, error)
	ReadStorageFile(ctx context.Context, storagePath string) (localstorage.ReadResult, error)
	DeleteStorageFile(ctx context.Context, storagePath string) (localstorage.Entry, error)
}

type AutomationRuntime interface {
	ClientRuntime
	CreateAutomationJob(ctx context.Context, input automation.CreateJobInput) (automation.Job, error)
	ListAutomationJobs(ctx context.Context) ([]automation.Job, error)
	PauseAutomationJob(ctx context.Context, jobID string) (automation.Job, error)
	ResumeAutomationJob(ctx context.Context, jobID string) (automation.Job, error)
	CompleteAutomationJob(ctx context.Context, jobID string) (automation.Job, error)
	DeleteAutomationJob(ctx context.Context, jobID string) (automation.Job, error)
	RunAutomationJobNow(ctx context.Context, jobID string) (automation.Fire, error)
}

type ServerRuntime interface {
	ServerStatus(ctx context.Context) (core.ServerStatus, error)
	RestartDaemon(ctx context.Context) error
}

type Result struct {
	Handled        bool
	Text           string
	ReloadSnapshot bool
	Picker         *PickerData
	Form           *FormData
	Prompt         *PromptData
	Confirm        *ConfirmData
	Info           *InfoData
}

type Dispatcher struct {
	configured     bool
	sessions       SessionRuntime
	externalAgents ExternalAgentRuntime
	providers      ProviderRuntime
	permissions    PermissionRuntime
	messages       SessionMessageRuntime
	contextRuntime ContextRuntime
	usage          UsageRuntime
	plan           PlanRuntime
	search         SearchRuntime
	storage        StorageRuntime
	automation     AutomationRuntime
	server         ServerRuntime
	workingDir     string
	now            func() time.Time
}

func New(runtime any, workingDir string) *Dispatcher {
	d := &Dispatcher{
		configured: runtime != nil,
		workingDir: strings.TrimSpace(workingDir),
		now:        time.Now,
	}
	if runtime != nil {
		d.sessions, _ = runtime.(SessionRuntime)
		d.externalAgents, _ = runtime.(ExternalAgentRuntime)
		d.providers, _ = runtime.(ProviderRuntime)
		d.permissions, _ = runtime.(PermissionRuntime)
		d.messages, _ = runtime.(SessionMessageRuntime)
		d.contextRuntime, _ = runtime.(ContextRuntime)
		d.usage, _ = runtime.(UsageRuntime)
		d.plan, _ = runtime.(PlanRuntime)
		d.search, _ = runtime.(SearchRuntime)
		d.storage, _ = runtime.(StorageRuntime)
		d.automation, _ = runtime.(AutomationRuntime)
		d.server, _ = runtime.(ServerRuntime)
	}
	return d
}

func unsupportedRuntime(area string) Result {
	area = strings.TrimSpace(area)
	if area == "" {
		return Result{Handled: true, Text: "Command runtime is not configured."}
	}
	return Result{Handled: true, Text: "Command runtime does not support " + area + " commands."}
}

func (d *Dispatcher) Handle(ctx context.Context, externalKey string, text string) (Result, error) {
	spec, args, ok := Parse(text)
	if !ok {
		return Result{}, nil
	}
	if d == nil || !d.configured {
		return Result{Handled: true, Text: "Command runtime is not configured."}, nil
	}

	switch spec.ID {
	case CommandHelp:
		return Result{Handled: true, Text: HelpText()}, nil
	case CommandNewSession:
		if d.sessions == nil {
			return unsupportedRuntime("sessions"), nil
		}
		return d.handleNewSession(ctx, externalKey, args)
	case CommandSessions:
		return d.handleSessions(ctx, externalKey)
	case CommandSession:
		return d.handleSession(ctx, externalKey, args)
	case CommandProvider:
		return d.handleProvider(ctx, externalKey, args)
	case CommandPermissions:
		return d.handlePermissions(ctx, externalKey, args)
	case CommandContext:
		return d.handleContext(ctx, externalKey, args)
	case CommandUsage:
		return d.handleUsage(ctx, externalKey)
	case CommandPlan:
		return d.handlePlan(ctx, externalKey, args)
	case CommandSearch:
		return d.handleSearch(ctx, externalKey, args)
	case CommandModules:
		return d.handleModules(ctx, args)
	case CommandRemind:
		return d.handleRemind(ctx, externalKey, args)
	case CommandTasks:
		return d.handleTasks(ctx, externalKey, args)
	case CommandServer:
		return d.handleServer(), nil
	case CommandStatus:
		return d.handleStatus(ctx)
	case CommandRestart:
		return d.handleRestart(ctx, args)
	default:
		return Result{Handled: true, Text: "Unknown command.\n\n" + HelpText()}, nil
	}
}
