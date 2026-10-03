package core

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/ids"
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
	skillsContext   SkillsPromptContextProvider
	runtimeStatus   RuntimeStatusContextProvider
	events          *eventBus
	now             func() time.Time
	newID           func(prefix string) string
	lifetime        context.Context
	budgets         RunBudgets
	sessionFiles    string
	compactProvider string
	compactModel    string
	windowCap       int
	// modelSlots bounds the model requests of all native runs at once.
	modelSlots *toolsched.Semaphore
	// toolLocks serialises tool calls sharing a concurrency key across runs.
	toolLocks *toolsched.Locks
	// compactUnavailable is set while the compact model cannot be resolved,
	// so the failure is logged once.
	compactUnavailable atomic.Bool

	// badBoundaries holds the IDs of unreadable boundaries already logged.
	badBoundaries sync.Map
	// contextUsage is the ContextUsage last announced per session ID.
	contextUsage sync.Map
	// liveTasks are the shell tasks this daemon started that still run.
	tasksMu   sync.Mutex
	liveTasks map[string]*liveTask
	// backgroundTasks bounds the background commands one session runs.
	backgroundTasks int
	// backgroundAgents bounds the background subagents of one session.
	backgroundAgents int
	// executing counts the runs goroutineStarter started; once stopped it
	// starts no more.
	executing sync.WaitGroup
	stopped   bool
	// startingAgents are the children each parent session is starting, by
	// name, and whether each runs in the background; guarded by mu.
	startingAgents map[string]map[string]bool
	// runEnds are the callers waiting for each run to end; guarded by mu.
	runEnds map[string][]chan struct{}
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

func New(store Store) *Core {
	c := &Core{
		store:            store,
		activeRuns:       map[string]*activeRun{},
		scheduledRuns:    map[string]time.Time{},
		sessionGates:     map[string]*sessionGate{},
		liveTasks:        map[string]*liveTask{},
		runEnds:          map[string][]chan struct{}{},
		backgroundTasks:  DefaultBackgroundTasks,
		backgroundAgents: DefaultBackgroundAgents,
		events:           newEventBus(),
		now:              time.Now,
		newID:            ids.New,
		lifetime:         context.Background(),
		budgets:          DefaultRunBudgets(),
		modelSlots:       toolsched.NewSemaphore(DefaultModelConcurrency),
		toolLocks:        toolsched.NewLocks(),
	}
	c.runStarter = goroutineStarter{c}
	return c
}

// The With* builder methods below configure a Core during single-threaded
// construction, before the daemon starts serving. Except for WithSessionLLMs,
// they mutate Core fields without holding c.mu and therefore MUST NOT be called
// after any run has started or after the Core is shared across goroutines —
// doing so races with the agent loop reading those fields. Post-construction
// mutation must go through the locked Set* methods (SetSessionLLMs,
// SetAssistantProfile, SetExternalAgents).
func (c *Core) WithAttachmentReader(reader agentcontext.AttachmentReader) *Core {
	if reader != nil {
		c.attachments = reader
	}
	return c
}

func (c *Core) WithClock(now func() time.Time) *Core {
	if now != nil {
		c.now = now
	}
	return c
}

// WithLifetime sets the daemon lifetime; interrupted runs are rescheduled only while it is alive.
func (c *Core) WithLifetime(ctx context.Context) *Core {
	if ctx != nil {
		c.lifetime = ctx
	}
	return c
}

func (c *Core) WithRunStarter(starter RunStarter) *Core {
	if starter != nil {
		c.runStarter = starter
	}
	return c
}

func (c *Core) WithExternalAgents(registry *externalagents.Registry, store externalagents.AttachmentStore) *Core {
	c.externalStore = store
	c.SetExternalAgents(registry)
	return c
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

func (c *Core) WithSessionLLMs(registry SessionLLMRegistry) *Core {
	if registry != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.llms = registry
	}
	return c
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

func (c *Core) WithTools(toolExecutor ToolExecutor) *Core {
	if toolExecutor != nil {
		c.tools = toolExecutor
	}
	return c
}

func (c *Core) WithSkillsContext(provider SkillsPromptContextProvider) *Core {
	if provider != nil {
		c.skillsContext = provider
	}
	return c
}

func (c *Core) WithRuntimeStatusContext(provider RuntimeStatusContextProvider) *Core {
	if provider != nil {
		c.runtimeStatus = provider
	}
	return c
}
