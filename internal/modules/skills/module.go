package skills

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	coreskills "github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const moduleID = "skills"

type Module struct {
	service *coreskills.Service
}

func New(cfg coreskills.Config) (*Module, error) {
	service, err := coreskills.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &Module{service: service}, nil
}

func (m *Module) Close() error {
	if m == nil || m.service == nil {
		return nil
	}
	return m.service.Close()
}

func (m *Module) ID() string {
	return moduleID
}

func (m *Module) Name() string {
	return "Skills"
}

func (m *Module) Service() *coreskills.Service {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterTools(registry *tools.Registry) error {
	if m == nil || m.service == nil || registry == nil {
		return nil
	}
	return registry.Register(coreskills.ToolExecutors(m.service)...)
}

func (m *Module) Context() string {
	if m == nil || m.service == nil {
		return ""
	}
	return strings.TrimSpace(`Skills: skill_search finds trusted workflows; skill_use activates one for this session; skill_manage creates/edits skills only through approval. Quarantined skills are inactive until trusted.`)
}

func (m *Module) SkillsPromptContext(_ context.Context, req core.SkillsPromptContextRequest) string {
	if m == nil || m.service == nil {
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
