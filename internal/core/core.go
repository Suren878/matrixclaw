package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/orchestration"
)

type Core struct {
	mu           sync.RWMutex
	store        Store
	orchestrator orchestration.RunStarter
	llms         SessionLLMRegistry
	assistant    AssistantProfile
	attachments  AttachmentReader
	activeRuns   map[string]*activeRun
	tools        ToolExecutor
	events       *eventBus
	now          func() time.Time
	newID        func(prefix string) string
	historyLimit int
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
		events:       newEventBus(),
		now:          time.Now,
		newID:        defaultID,
		historyLimit: 50,
	}
}

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

func (c *Core) WithOrchestrator(orchestrator orchestration.RunStarter) *Core {
	if orchestrator != nil {
		c.orchestrator = orchestrator
	}
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
