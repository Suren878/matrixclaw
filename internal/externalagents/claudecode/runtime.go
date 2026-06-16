package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/safego"
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

type promptJSONOutput struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

const (
	defaultApprovalPolicy = "never"
	defaultSandbox        = "danger-full-access"
)

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
		AgentID:        AgentID,
		CWD:            cwd,
		Model:          model,
		ApprovalPolicy: defaultString(req.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:        defaultString(req.Sandbox, defaultSandbox),
		Metadata:       map[string]any{"mode": "cli"},
	}, nil
}

func (r *Runtime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	session.AgentID = AgentID
	session.ApprovalPolicy = defaultString(session.ApprovalPolicy, defaultApprovalPolicy)
	session.Sandbox = defaultString(session.Sandbox, defaultSandbox)
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
	safego.Go("claudecode.runPrompt", func() { r.runPrompt(ctx, out, resolved, session, text) })
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
	if !safego.Run("claudecode.runPrompt", func() {
		r.runPromptCommand(ctx, out, path, session, text, turnID)
	}) {
		sessionID := claudeSessionID(session)
		out <- externalagents.Event{
			Kind:              externalagents.EventTurnFailed,
			AgentID:           AgentID,
			ExternalThreadID:  sessionID,
			ExternalSessionID: sessionID,
			ExternalTurnID:    turnID,
			Error:             "claudecode prompt worker panicked",
			At:                time.Now().UTC(),
		}
	}
}

func (r *Runtime) runPromptCommand(ctx context.Context, out chan<- externalagents.Event, path string, session externalagents.ExternalSession, text string, turnID string) {
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
			Kind:              externalagents.EventTurnFailed,
			AgentID:           AgentID,
			ExternalThreadID:  claudeSessionID(session),
			ExternalSessionID: claudeSessionID(session),
			ExternalTurnID:    turnID,
			Error:             message,
			At:                time.Now().UTC(),
		}
		return
	}
	output, err := parseClaudePromptOutput(stdout.Bytes())
	if err != nil {
		out <- externalagents.Event{
			Kind:              externalagents.EventTurnFailed,
			AgentID:           AgentID,
			ExternalThreadID:  claudeSessionID(session),
			ExternalSessionID: claudeSessionID(session),
			ExternalTurnID:    turnID,
			Error:             err.Error(),
			At:                time.Now().UTC(),
		}
		return
	}
	sessionID := firstClaudeSessionID(output.SessionID, session)
	if strings.TrimSpace(sessionID) != "" {
		out <- externalagents.Event{
			Kind:              externalagents.EventTurnStarted,
			AgentID:           AgentID,
			ExternalThreadID:  sessionID,
			ExternalSessionID: sessionID,
			ExternalTurnID:    turnID,
			At:                time.Now().UTC(),
		}
	}
	if value := strings.TrimSpace(output.Result); value != "" {
		out <- externalagents.Event{
			Kind:              externalagents.EventMessageDelta,
			AgentID:           AgentID,
			ExternalThreadID:  sessionID,
			ExternalSessionID: sessionID,
			ExternalTurnID:    turnID,
			Text:              value,
			At:                time.Now().UTC(),
		}
	}
	out <- externalagents.Event{
		Kind:              externalagents.EventTurnCompleted,
		AgentID:           AgentID,
		ExternalThreadID:  sessionID,
		ExternalSessionID: sessionID,
		ExternalTurnID:    turnID,
		At:                time.Now().UTC(),
	}
}

func claudePromptArgs(session externalagents.ExternalSession, text string) []string {
	args := []string{"-p", text, "--output-format", "json"}
	if sessionID := claudeSessionID(session); sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if model := strings.TrimSpace(session.Model); model != "" {
		args = append(args, "--model", model)
	}
	if mode := claudePermissionMode(session); mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	return args
}

func claudePermissionMode(session externalagents.ExternalSession) string {
	approvalPolicy := strings.ToLower(strings.TrimSpace(session.ApprovalPolicy))
	sandbox := strings.ToLower(strings.TrimSpace(session.Sandbox))
	switch {
	case approvalPolicy == "never" || sandbox == "danger-full-access":
		return "bypassPermissions"
	case approvalPolicy == "on-request" && sandbox == "workspace-write":
		return "acceptEdits"
	default:
		return "default"
	}
}

func defaultString(value string, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func parseClaudePromptOutput(data []byte) (promptJSONOutput, error) {
	var output promptJSONOutput
	if err := json.Unmarshal(data, &output); err != nil {
		text := strings.TrimSpace(string(data))
		if text == "" {
			return promptJSONOutput{}, fmt.Errorf("claudecode: empty prompt output")
		}
		return promptJSONOutput{Result: text}, nil
	}
	if strings.TrimSpace(output.Error) != "" {
		return promptJSONOutput{}, fmt.Errorf("claudecode: %s", strings.TrimSpace(output.Error))
	}
	return output, nil
}

func claudeSessionID(session externalagents.ExternalSession) string {
	for _, value := range []string{session.ExternalSessionID, session.ExternalThreadID} {
		value = strings.TrimSpace(value)
		if value != "" && !strings.HasPrefix(value, "claude-") {
			return value
		}
	}
	return ""
}

func firstClaudeSessionID(value string, session externalagents.ExternalSession) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return claudeSessionID(session)
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
