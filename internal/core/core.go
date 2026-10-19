package core

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/hooks"
	"github.com/Suren878/matrixclaw/internal/ids"
	"github.com/Suren878/matrixclaw/internal/memory"
	"github.com/Suren878/matrixclaw/internal/sandbox"
)

type Core struct {
	mu              sync.RWMutex
	store           Store
	runStarter      RunStarter
	llms            SessionLLMRegistry
	assistant       AssistantProfile
	attachments     agentcontext.AttachmentReader
	externalAgents  *externalagents.Registry
	externalStore   externalagents.AttachmentStore
	activeRuns      map[string]*activeRun
	scheduledRuns   map[string]time.Time
	sessionGates    map[string]*sessionGate
	tools           ToolExecutor
	lspAvailable    func(workingDir string) bool
	skillsContext   SkillsPromptContextProvider
	memory          Memory
	runtimeStatus   RuntimeStatusContextProvider
	events          *eventBus
	now             func() time.Time
	newID           func(prefix string) string
	lifetime        context.Context
	effectiveTuning atomic.Pointer[Tuning]
	sessionFiles    string
	// promptEnv is where native runs are told they work.
	promptEnv PromptEnvironment
	// modelSlots bounds the model requests of all native runs at once.
	modelSlots *toolsched.Semaphore
	// toolLocks serialises tool calls sharing a concurrency key across runs.
	toolLocks *toolsched.Locks
	// compactUnavailable is set while the compact model cannot be resolved,
	// so the failure is logged once.
	compactUnavailable atomic.Bool

	// agentFileErrors holds the agent file errors already logged.
	agentFileErrors sync.Map
	// worktrees holds the git worktrees of worktree subagents.
	worktrees string
	// badBoundaries holds the IDs of unreadable boundaries already logged.
	badBoundaries sync.Map
	// contextUsage is the ContextUsage last announced per session ID.
	contextUsage sync.Map
	// liveTasks are the shell tasks this daemon started that still run.
	tasksMu   sync.Mutex
	liveTasks map[string]*liveTask
	// commandEnv is the environment of shell commands; nil inherits the daemon's.
	commandEnv func() []string
	// sandbox confines shell commands; nil leaves them unconfined.
	sandbox func() sandbox.Settings
	// hooks runs the user's hooks and the checks after edits; nil runs none.
	hooks *hooks.Runner
	// executing counts the runs goroutineStarter started and the session
	// reviews; once stopped it starts no more.
	executing sync.WaitGroup
	stopped   bool
	// startingAgents are the children each parent session is starting, by
	// name, and whether each runs in the background; guarded by mu.
	startingAgents map[string]map[string]bool
	// runEnds are the callers waiting for each run to end, and runsEnding
	// counts each run's ending transitions not followed up yet; guarded by mu.
	runEnds    map[string][]chan struct{}
	runsEnding map[string]int
	// checkpoints snapshots working directories before tool batches that
	// change files; nil when file checkpoints are off.
	checkpoints *checkpoint.Store
	// checkpointFailures holds the snapshot failures already logged.
	checkpointFailures sync.Map
	// reviews are the background reviews of idle sessions.
	reviews sessionReviews
}

type SkillsPromptContextRequest struct {
	SessionID  string
	RunID      string
	WorkingDir string
	Messages   []SkillsPromptMessage
}

type SkillsPromptMessage struct {
	Role    string
	Content string
}

type SkillsPromptContextProvider interface {
	SkillsPromptContext(context.Context, SkillsPromptContextRequest) string
}

// Memory is the persistent memory native runs are shown and session reviews
// suggest entries to.
type Memory interface {
	List(ctx context.Context, filter memory.Filter) ([]memory.Entry, error)
	Create(ctx context.Context, entry memory.Entry) (memory.Entry, error)
	PromptContext(ctx context.Context, workingDir string) (string, error)
}

type RuntimeStatusContextRequest struct {
	SessionID  string
	RunID      string
	WorkingDir string
	ToolIDs    []string
}

type RuntimeStatusContextProvider interface {
	RuntimeStatusPromptContext(context.Context, RuntimeStatusContextRequest) string
}

type AssistantProfile struct {
	Name               string
	SystemPrompt       string
	CustomInstructions string
}

// Config holds what a Core is built with. Each field is read while runs
// execute, so a Core takes them once here; what changes while the daemon
// serves, or is built on the Core itself, goes through the Set* methods.
type Config struct {
	Store Store
	// Clock defaults to time.Now.
	Clock func() time.Time
	// Lifetime is the daemon's; interrupted runs are rescheduled only while
	// it is alive. It defaults to context.Background.
	Lifetime context.Context
	// RunStarter defaults to executing each run in a goroutine of its own.
	RunStarter  RunStarter
	Attachments agentcontext.AttachmentReader
	// ExternalAgents is the registry runs start with; SetExternalAgents swaps it.
	ExternalAgents     *externalagents.Registry
	ExternalAgentStore externalagents.AttachmentStore
	SkillsContext      SkillsPromptContextProvider
	// Memory is shown to native runs; nil shows none.
	Memory Memory
	// SkillLearning turns on session reviews, which file what they find with it.
	SkillLearning SkillLearning
	// PromptEnvironment is where native runs are told they work; without a
	// timezone their clock reads in UTC.
	PromptEnvironment PromptEnvironment
	// SessionFiles holds per-session files such as full tool outputs; a
	// session's files are removed with it.
	SessionFiles string
	// Worktrees holds the git worktrees of worktree subagents; without it
	// worktree isolation is unavailable.
	Worktrees string
	// Checkpoints snapshots working directories before tool batches that
	// change files; nil takes none.
	Checkpoints *checkpoint.Store
	// CommandEnvironment is read for every shell command; nil inherits the
	// daemon's environment.
	CommandEnvironment func() []string
	// Sandbox is read for every shell command; nil leaves commands unconfined.
	Sandbox func() sandbox.Settings
	// Hooks runs the user's hooks and the checks after edits; nil runs none.
	Hooks *hooks.Runner
	// LSPAvailable says whether a language server serves a project; nil never
	// offers the lsp tool.
	LSPAvailable func(workingDir string) bool
}

func New(cfg Config) *Core {
	c := &Core{
		store:          cfg.Store,
		runStarter:     cfg.RunStarter,
		attachments:    cfg.Attachments,
		externalAgents: cfg.ExternalAgents,
		externalStore:  cfg.ExternalAgentStore,
		skillsContext:  cfg.SkillsContext,
		memory:         cfg.Memory,
		promptEnv:      cfg.PromptEnvironment,
		sessionFiles:   strings.TrimSpace(cfg.SessionFiles),
		worktrees:      cfg.Worktrees,
		checkpoints:    cfg.Checkpoints,
		commandEnv:     cfg.CommandEnvironment,
		sandbox:        cfg.Sandbox,
		hooks:          cfg.Hooks,
		lspAvailable:   cfg.LSPAvailable,
		reviews:        sessionReviews{learning: cfg.SkillLearning},
		activeRuns:     map[string]*activeRun{},
		scheduledRuns:  map[string]time.Time{},
		sessionGates:   map[string]*sessionGate{},
		liveTasks:      map[string]*liveTask{},
		runEnds:        map[string][]chan struct{}{},
		runsEnding:     map[string]int{},
		events:         newEventBus(),
		now:            cfg.Clock,
		newID:          ids.New,
		lifetime:       cfg.Lifetime,
		modelSlots:     toolsched.NewSemaphore(DefaultModelConcurrency),
		toolLocks:      toolsched.NewLocks(),
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.lifetime == nil {
		c.lifetime = context.Background()
	}
	if c.runStarter == nil {
		c.runStarter = goroutineStarter{c}
	}
	c.SetTuning(Tuning{})
	return c
}

// SetTools wires the tool executor, which is built on this Core; call it
// before the Core serves.
func (c *Core) SetTools(tools ToolExecutor) {
	c.tools = tools
}

// SetRuntimeStatusContext wires the note on module state, whose provider
// is built on this Core; call it before the Core serves.
func (c *Core) SetRuntimeStatusContext(provider RuntimeStatusContextProvider) {
	c.runtimeStatus = provider
}

// SetExternalAgents swaps the external agent registry while the daemon serves.
func (c *Core) SetExternalAgents(registry *externalagents.Registry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.externalAgents = registry
}

func (c *Core) externalAgentRegistry() *externalagents.Registry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.externalAgents
}

func (c *Core) SetSessionLLMs(registry SessionLLMRegistry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.llms = registry
}

func (c *Core) SetAssistantProfile(profile AssistantProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.assistant = normalizeAssistantProfile(profile)
}

func (c *Core) assistantProfile() AssistantProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.assistant
}

func normalizeAssistantProfile(profile AssistantProfile) AssistantProfile {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.SystemPrompt = strings.TrimSpace(profile.SystemPrompt)
	profile.CustomInstructions = strings.TrimSpace(profile.CustomInstructions)
	return profile
}

func (c *Core) sessionLLMs() SessionLLMRegistry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.llms
}
