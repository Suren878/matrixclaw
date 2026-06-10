package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/work"
)

type Core struct {
	mu             sync.RWMutex
	store          Store
	workStore      work.Store
	runStarter     RunStarter
	llms           SessionLLMRegistry
	assistant      AssistantProfile
	attachments    AttachmentReader
	externalAgents *externalagents.Registry
	externalStore  externalagents.AttachmentStore
	activeRuns     map[string]*activeRun
	sessionGates   map[string]*sync.Mutex
	tools          ToolExecutor
	skillsContext  SkillsPromptContextProvider
	runtimeStatus  RuntimeStatusContextProvider
	events         *eventBus
	now            func() time.Time
	newID          func(prefix string) string
	historyLimit   int
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

type AttachmentData struct {
	Data     []byte
	MIMEType string
	Name     string
	Size     int64
}

type AttachmentReader interface {
	ReadAttachment(ctx context.Context, path string, temporary bool, maxBytes int64) (AttachmentData, error)
}

func New(store Store) *Core {
	return &Core{
		store:        store,
		activeRuns:   map[string]*activeRun{},
		sessionGates: map[string]*sync.Mutex{},
		events:       newEventBus(),
		now:          time.Now,
		newID:        defaultID,
		historyLimit: 50,
	}
}

// The With* builder methods below configure a Core during single-threaded
// construction, before the daemon starts serving. Except for WithSessionLLMs,
// they mutate Core fields without holding c.mu and therefore MUST NOT be called
// after any run has started or after the Core is shared across goroutines —
// doing so races with the agent loop reading those fields. Post-construction
// mutation must go through the locked Set* methods (SetSessionLLMs,
// SetAssistantProfile).
func (c *Core) WithAttachmentReader(reader AttachmentReader) *Core {
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

func (c *Core) WithIDGenerator(newID func(prefix string) string) *Core {
	if newID != nil {
		c.newID = newID
	}
	return c
}

func (c *Core) WithRunStarter(starter RunStarter) *Core {
	if starter != nil {
		c.runStarter = starter
	}
	return c
}

func (c *Core) WithWorkStore(store work.Store) *Core {
	if store != nil {
		c.workStore = store
	}
	return c
}

func (c *Core) WithExternalAgents(registry *externalagents.Registry, store externalagents.AttachmentStore) *Core {
	c.externalAgents = registry
	c.externalStore = store
	return c
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

func (c *Core) WithHistoryLimit(limit int) *Core {
	if limit > 0 {
		c.historyLimit = limit
	}
	return c
}

func defaultID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}
