package skills

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	coreskills "github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type Module struct {
	service *coreskills.Service
	enabled atomic.Bool
}

func New(cfg coreskills.Config) (*Module, error) {
	service, err := coreskills.NewService(cfg)
	if err != nil {
		return nil, err
	}
	module := &Module{service: service}
	module.enabled.Store(cfg.Enabled)
	return module, nil
}

func (m *Module) ID() string { return "skills" }

// Apply shows or hides the skill tools; the trust policy and auto-invoke
// settings apply at the next start.
func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	m.enabled.Store(cfg.Modules.Skills.IsEnabled())
	return nil
}

func (m *Module) Status(context.Context) modules.Status {
	enabled := m.enabled.Load()
	state := "Disabled"
	if enabled {
		state = "Ready"
	}
	return modules.Status{ID: m.ID(), Title: "Skills", Enabled: enabled, Ready: enabled, State: state}
}

func (m *Module) Close() error {
	return m.service.Close()
}

func (m *Module) Service() *coreskills.Service {
	return m.service
}

func (m *Module) Tools() []tools.Executor {
	if !m.enabled.Load() {
		return nil
	}
	return coreskills.ToolExecutors(m.service)
}

func (m *Module) Context() string {
	if !m.enabled.Load() {
		return ""
	}
	return strings.TrimSpace(`Skills: skill_search finds trusted workflows; skill_use activates one for this session; skill_manage creates/edits skills only through approval. Quarantined skills are inactive until trusted.`)
}

func (m *Module) SkillsPromptContext(_ context.Context, req core.SkillsPromptContextRequest) string {
	if !m.enabled.Load() {
		return ""
	}
	messages := make([]coreskills.PromptMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		messages = append(messages, coreskills.PromptMessage{Role: message.Role, Content: message.Content})
	}
	return m.service.PromptContext(coreskills.PromptRequest{
		SessionID:  req.SessionID,
		RunID:      req.RunID,
		WorkingDir: req.WorkingDir,
		Messages:   messages,
	})
}
