package skills

import (
	"context"
	"database/sql"
	"os"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	coreskills "github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type Module struct {
	service *coreskills.Service
	enabled atomic.Bool
	review  atomic.Pointer[core.SessionReviewSettings]
}

// Session review defaults, unless setup.json sets them.
const (
	defaultReviewToolSteps = 10
	defaultReviewInterval  = 6 * time.Hour
)

// New opens the library at root on the store's database; it is off until
// the first Apply.
func New(db *sql.DB, root string) (*Module, error) {
	service, err := coreskills.NewService(db, root)
	if err != nil {
		return nil, err
	}
	module := &Module{service: service}
	module.review.Store(&core.SessionReviewSettings{})
	return module, nil
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
	review := core.SessionReviewSettings{
		Enabled:      skills.IsReviewEnabled(),
		MinToolSteps: defaultReviewToolSteps,
		Interval:     defaultReviewInterval,
	}
	if steps := skills.Review.MinToolSteps; steps > 0 {
		review.MinToolSteps = steps
	}
	if hours := skills.Review.IntervalHours; hours > 0 {
		review.Interval = time.Duration(hours) * time.Hour
	}
	m.review.Store(&review)
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

// ReviewSettings are the session review's settings from setup.json.
func (m *Module) ReviewSettings() core.SessionReviewSettings {
	return *m.review.Load()
}

// ReviewedSkills are the skills the session activated, with their instructions.
func (m *Module) ReviewedSkills(ctx context.Context, sessionID string) ([]core.ReviewedSkill, error) {
	active, err := m.service.SessionSkills(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	reviewed := make([]core.ReviewedSkill, 0, len(active))
	for _, skill := range active {
		detail, err := m.service.Get(ctx, skill.ID)
		if err != nil {
			return nil, err
		}
		reviewed = append(reviewed, core.ReviewedSkill{ID: skill.ID, Name: skill.Name, Description: skill.Description, Body: detail.Body})
	}
	return reviewed, nil
}

// DraftProposal files a session review's skill as a quarantined draft tagged
// learned, and update-of-<id> when it rewrites a skill.
func (m *Module) DraftProposal(ctx context.Context, proposal core.SkillProposal) (string, error) {
	tags := []string{"learned"}
	if proposal.Updates != "" {
		tags = append(tags, "update-of-"+proposal.Updates)
	}
	draft, err := m.service.CreateDraft(ctx, proposal.Name, proposal.Description, tags, proposal.Body)
	return draft.ID, err
}
