package skills

import (
	"context"
	"database/sql"
	"os"
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

// New opens the library at root on the store's database; it is off until
// the first Apply.
func New(db *sql.DB, root string) (*Module, error) {
	service, err := coreskills.NewService(db, root)
	if err != nil {
		return nil, err
	}
	return &Module{service: service}, nil
}

func (m *Module) ID() string { return "skills" }

// Apply shows or hides the skill tools and applies the library settings.
func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	skills := cfg.Modules.Skills
	m.service.Configure(coreskills.Settings{
		AutoInvoke:  skills.IsAutoInvoke(),
		TrustPolicy: skills.TrustPolicy,
		GitEnv:      setup.ChildEnvironment(cfg, os.Environ()),
	})
	m.enabled.Store(skills.IsEnabled())
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
	return nil
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

func (m *Module) SkillsPromptContext(ctx context.Context, req core.SkillsPromptContextRequest) string {
	if !m.enabled.Load() {
		return ""
	}
	messages := make([]coreskills.PromptMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		messages = append(messages, coreskills.PromptMessage{Role: message.Role, Content: message.Content})
	}
	return m.service.PromptContext(ctx, coreskills.PromptRequest{
		SessionID:  req.SessionID,
		RunID:      req.RunID,
		WorkingDir: req.WorkingDir,
		Messages:   messages,
	})
}
