package claudecode

import (
	"bufio"
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
	"github.com/Suren878/matrixclaw/internal/textutil"
)

type Runtime struct {
	path    string
	enabled bool
	binary  *externalagents.BinaryProbe
	stderr  io.Writer
}

type RuntimeOptions struct {
	Path    string
	Enabled bool
	Stderr  io.Writer
}

type streamJSONOutput struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Result    string          `json:"result"`
	Error     string          `json:"error"`
	IsError   bool            `json:"is_error"`
	Event     json.RawMessage `json:"event"`
	Message   json.RawMessage `json:"message"`
}

type streamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
}

type streamAssistantMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

const (
	defaultApprovalPolicy = "never"
	defaultSandbox        = "danger-full-access"
	claudeStderrLimit     = 256 * 1024
)

type cappedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || len(data) > 0
		return written, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(data)
	return written, nil
}

func (b *cappedBuffer) String() string {
	value := b.buffer.String()
	if b.truncated {
		value += "\n[MatrixClaw: Claude stderr truncated]"
	}
	return value
}

func NewRuntime(opts RuntimeOptions) *Runtime {
	return &Runtime{
		path:    opts.Path,
		enabled: opts.Enabled,
		binary:  externalagents.NewBinaryProbe("claude", opts.Path),
		stderr:  opts.Stderr,
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
		ApprovalPolicy: textutil.FirstNonEmpty(req.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:        textutil.FirstNonEmpty(req.Sandbox, defaultSandbox),
		Metadata:       map[string]any{"mode": "cli"},
	}, nil
}

func (r *Runtime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	session.AgentID = AgentID
	session.ApprovalPolicy = textutil.FirstNonEmpty(session.ApprovalPolicy, defaultApprovalPolicy)
	session.Sandbox = textutil.FirstNonEmpty(session.Sandbox, defaultSandbox)
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
	resolved, err := externalagents.LookupBinary("claude", r.path)
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
		sendClaudeEvent(ctx, out, externalagents.Event{
			Kind:              externalagents.EventTurnFailed,
			AgentID:           AgentID,
			ExternalThreadID:  sessionID,
			ExternalSessionID: sessionID,
			ExternalTurnID:    turnID,
			Error:             "claudecode prompt worker panicked",
			At:                time.Now().UTC(),
		})
	}
}

func (r *Runtime) runPromptCommand(ctx context.Context, out chan<- externalagents.Event, path string, session externalagents.ExternalSession, text string, turnID string) {
	args := claudePromptArgs(session, text)
	cmd := exec.CommandContext(ctx, path, args...)
	if strings.TrimSpace(session.CWD) != "" {
		cmd.Dir = strings.TrimSpace(session.CWD)
	}
	stderr := &cappedBuffer{limit: claudeStderrLimit}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		emitClaudeFailure(ctx, out, session, turnID, err.Error())
		return
	}
	if r.stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, r.stderr)
	} else {
		cmd.Stderr = stderr
	}
	if err := cmd.Start(); err != nil {
		emitClaudeFailure(ctx, out, session, turnID, err.Error())
		return
	}

	sessionID := claudeSessionID(session)
	if !emitClaudeStarted(ctx, out, sessionID, turnID) {
		_ = cmd.Wait()
		return
	}
	streamedText := false
	assistantFallback := ""
	var result streamJSONOutput
	sawResult := false
	var lastParseErr error
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		envelope, parseErr := parseClaudeStreamOutput(scanner.Bytes())
		if parseErr != nil {
			lastParseErr = parseErr
			continue
		}
		if value := strings.TrimSpace(envelope.SessionID); value != "" && value != sessionID {
			sessionID = value
			if !emitClaudeStarted(ctx, out, sessionID, turnID) {
				_ = cmd.Wait()
				return
			}
		}
		switch envelope.Type {
		case "stream_event":
			kind, delta := claudeStreamDelta(envelope.Event)
			if delta == "" {
				continue
			}
			if kind == externalagents.EventMessageDelta {
				streamedText = true
			}
			if !sendClaudeEvent(ctx, out, externalagents.Event{
				Kind:              kind,
				AgentID:           AgentID,
				ExternalThreadID:  sessionID,
				ExternalSessionID: sessionID,
				ExternalTurnID:    turnID,
				Text:              delta,
				At:                time.Now().UTC(),
			}) {
				_ = cmd.Wait()
				return
			}
		case "assistant":
			if value := claudeAssistantText(envelope.Message); value != "" {
				assistantFallback = value
			}
		case "result":
			result = envelope
			sawResult = true
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if waitErr != nil || scanErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			if scanErr != nil {
				message = scanErr.Error()
			} else {
				message = waitErr.Error()
			}
		}
		emitClaudeFailureWithSession(ctx, out, sessionID, turnID, message)
		return
	}
	if !sawResult {
		message := "claudecode: stream ended without a result event"
		if lastParseErr != nil {
			message += ": " + lastParseErr.Error()
		}
		emitClaudeFailureWithSession(ctx, out, sessionID, turnID, message)
		return
	}
	if result.IsError || (result.Subtype != "" && result.Subtype != "success") || strings.TrimSpace(result.Error) != "" {
		message := textutil.FirstNonEmpty(result.Error, result.Result, "claudecode: turn failed")
		emitClaudeFailureWithSession(ctx, out, sessionID, turnID, message)
		return
	}
	if !streamedText {
		value := strings.TrimSpace(result.Result)
		if value == "" {
			value = assistantFallback
		}
		if value != "" {
			if !sendClaudeEvent(ctx, out, externalagents.Event{
				Kind:              externalagents.EventMessageDelta,
				AgentID:           AgentID,
				ExternalThreadID:  sessionID,
				ExternalSessionID: sessionID,
				ExternalTurnID:    turnID,
				Text:              value,
				At:                time.Now().UTC(),
			}) {
				return
			}
		}
	}
	sendClaudeEvent(ctx, out, externalagents.Event{
		Kind:              externalagents.EventTurnCompleted,
		AgentID:           AgentID,
		ExternalThreadID:  sessionID,
		ExternalSessionID: sessionID,
		ExternalTurnID:    turnID,
		At:                time.Now().UTC(),
	})
}

func emitClaudeStarted(ctx context.Context, out chan<- externalagents.Event, sessionID string, turnID string) bool {
	return sendClaudeEvent(ctx, out, externalagents.Event{
		Kind:              externalagents.EventTurnStarted,
		AgentID:           AgentID,
		ExternalThreadID:  sessionID,
		ExternalSessionID: sessionID,
		ExternalTurnID:    turnID,
		At:                time.Now().UTC(),
	})
}

func emitClaudeFailure(ctx context.Context, out chan<- externalagents.Event, session externalagents.ExternalSession, turnID string, message string) {
	emitClaudeFailureWithSession(ctx, out, claudeSessionID(session), turnID, message)
}

func emitClaudeFailureWithSession(ctx context.Context, out chan<- externalagents.Event, sessionID string, turnID string, message string) {
	sendClaudeEvent(ctx, out, externalagents.Event{
		Kind:              externalagents.EventTurnFailed,
		AgentID:           AgentID,
		ExternalThreadID:  sessionID,
		ExternalSessionID: sessionID,
		ExternalTurnID:    turnID,
		Error:             strings.TrimSpace(message),
		At:                time.Now().UTC(),
	})
}

func sendClaudeEvent(ctx context.Context, out chan<- externalagents.Event, event externalagents.Event) bool {
	if ctx == nil {
		out <- event
		return true
	}
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func parseClaudeStreamOutput(data []byte) (streamJSONOutput, error) {
	var output streamJSONOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return streamJSONOutput{}, err
	}
	return output, nil
}

func claudeStreamDelta(raw json.RawMessage) (externalagents.EventKind, string) {
	var event streamEvent
	if len(raw) == 0 || json.Unmarshal(raw, &event) != nil || event.Type != "content_block_delta" {
		return "", ""
	}
	switch event.Delta.Type {
	case "text_delta":
		return externalagents.EventMessageDelta, event.Delta.Text
	case "thinking_delta":
		return externalagents.EventReasoningDelta, event.Delta.Thinking
	default:
		return "", ""
	}
}

func claudeAssistantText(raw json.RawMessage) string {
	var message streamAssistantMessage
	if len(raw) == 0 || json.Unmarshal(raw, &message) != nil {
		return ""
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func claudePromptArgs(session externalagents.ExternalSession, text string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if sessionID := claudeSessionID(session); sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if model := strings.TrimSpace(session.Model); model != "" {
		args = append(args, "--model", model)
	}
	if mode := claudePermissionMode(session); mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	return append(args, "--", text)
}

func claudePermissionMode(session externalagents.ExternalSession) string {
	approvalPolicy := strings.ToLower(strings.TrimSpace(session.ApprovalPolicy))
	sandbox := strings.ToLower(strings.TrimSpace(session.Sandbox))
	switch {
	case sandbox == "danger-full-access":
		// Claude refuses bypassPermissions when the daemon runs as root. `auto`
		// is the supported autonomous mode and keeps the worker usable without
		// passing the root-forbidden --dangerously-skip-permissions path.
		return "auto"
	case approvalPolicy == "never":
		return "dontAsk"
	case approvalPolicy == "on-request" && sandbox == "workspace-write":
		return "acceptEdits"
	default:
		return "default"
	}
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

func defaultModel() string {
	return models()[0]
}

func newClaudeThreadID() string {
	return fmt.Sprintf("claude-%d", time.Now().UnixNano())
}
