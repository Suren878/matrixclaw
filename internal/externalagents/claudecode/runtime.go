package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

type Runtime struct {
	Agent
	stderr io.Writer
}

type RuntimeOptions struct {
	Path    string
	Enabled bool
	Stderr  io.Writer
}

func NewRuntime(opts RuntimeOptions) *Runtime {
	return &Runtime{
		Agent: Agent{
			Path:    opts.Path,
			Enabled: opts.Enabled,
		},
		stderr: opts.Stderr,
	}
}

func (r *Runtime) StartSession(_ context.Context, req externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	cwd := strings.TrimSpace(req.CWD)
	if cwd == "" {
		if current, err := os.Getwd(); err == nil {
			cwd = current
		}
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = defaultModel()
	}
	return externalagents.ExternalSession{
		AgentID:           AgentID,
		ExternalThreadID:  newClaudeThreadID(),
		ExternalSessionID: newClaudeThreadID(),
		CWD:               cwd,
		Model:             model,
		ApprovalPolicy:    strings.TrimSpace(req.ApprovalPolicy),
		Sandbox:           strings.TrimSpace(req.Sandbox),
		Metadata:          map[string]any{"mode": "cli"},
	}, nil
}

func (r *Runtime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	if strings.TrimSpace(session.ExternalThreadID) == "" {
		session.ExternalThreadID = newClaudeThreadID()
	}
	if strings.TrimSpace(session.ExternalSessionID) == "" {
		session.ExternalSessionID = session.ExternalThreadID
	}
	session.AgentID = AgentID
	if session.Metadata == nil {
		session.Metadata = map[string]any{"mode": "cli"}
	}
	return session, nil
}

func (r *Runtime) Send(ctx context.Context, session externalagents.ExternalSession, input externalagents.Input) (<-chan externalagents.Event, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, fmt.Errorf("claudecode: input text is required")
	}
	resolved, err := LookupPath(r.Path)
	if err != nil {
		return nil, fmt.Errorf("claudecode: claude binary not found: %w", err)
	}
	session, err = r.ResumeSession(ctx, session)
	if err != nil {
		return nil, err
	}
	out := make(chan externalagents.Event, 4)
	go r.runPrompt(ctx, out, resolved, session, text)
	return out, nil
}

func (r *Runtime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return fmt.Errorf("claudecode: interrupt is not implemented")
}

func (r *Runtime) Close() error {
	return nil
}

func (r *Runtime) runPrompt(ctx context.Context, out chan<- externalagents.Event, path string, session externalagents.ExternalSession, text string) {
	defer close(out)
	turnID := newClaudeThreadID()
	args := claudePromptArgs(session, text)
	cmd := exec.CommandContext(ctx, path, args...)
	if strings.TrimSpace(session.CWD) != "" {
		cmd.Dir = strings.TrimSpace(session.CWD)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	if r.stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, r.stderr)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		out <- externalagents.Event{
			Kind:             externalagents.EventTurnFailed,
			AgentID:          AgentID,
			ExternalThreadID: session.ExternalThreadID,
			ExternalTurnID:   turnID,
			Error:            message,
			At:               time.Now().UTC(),
		}
		return
	}
	if value := stdout.String(); value != "" {
		out <- externalagents.Event{
			Kind:             externalagents.EventMessageDelta,
			AgentID:          AgentID,
			ExternalThreadID: session.ExternalThreadID,
			ExternalTurnID:   turnID,
			Text:             value,
			At:               time.Now().UTC(),
		}
	}
	out <- externalagents.Event{
		Kind:             externalagents.EventTurnCompleted,
		AgentID:          AgentID,
		ExternalThreadID: session.ExternalThreadID,
		ExternalTurnID:   turnID,
		At:               time.Now().UTC(),
	}
}

func claudePromptArgs(session externalagents.ExternalSession, text string) []string {
	args := []string{"-p", text}
	if model := strings.TrimSpace(session.Model); model != "" {
		args = append(args, "--model", model)
	}
	return args
}

func defaultModel() string {
	models := Agent{}.Models(context.Background())
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func newClaudeThreadID() string {
	return fmt.Sprintf("claude-%d", time.Now().UnixNano())
}
