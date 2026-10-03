package codexapp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

type Runtime struct {
	path    string
	enabled bool
	binary  *externalagents.BinaryProbe

	mu          sync.Mutex
	client      *Client
	ownsClient  bool
	initialized bool
	stderr      io.Writer
	activeTurns map[string]string
}

type RuntimeOptions struct {
	Path    string
	Enabled bool
	Stderr  io.Writer
	Client  *Client
}

const (
	defaultApprovalPolicy = "never"
	defaultSandbox        = "danger-full-access"
	initializeTimeout     = 30 * time.Second
)

func NewRuntime(opts RuntimeOptions) *Runtime {
	return &Runtime{
		path:        opts.Path,
		enabled:     opts.Enabled,
		binary:      externalagents.NewBinaryProbe("codex", opts.Path),
		client:      opts.Client,
		ownsClient:  opts.Client == nil,
		stderr:      opts.Stderr,
		activeTurns: map[string]string{},
	}
}

func (r *Runtime) StartSession(ctx context.Context, req externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	client, err := r.ensureClient(ctx)
	if err != nil {
		return externalagents.ExternalSession{}, err
	}
	resp, err := client.StartThread(ctx, ThreadStartParams{
		Model:                 strings.TrimSpace(req.Model),
		CWD:                   strings.TrimSpace(req.CWD),
		ApprovalPolicy:        textutil.FirstNonEmpty(req.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:               textutil.FirstNonEmpty(req.Sandbox, defaultSandbox),
		BaseInstructions:      req.BaseInstructions,
		DeveloperInstructions: req.DeveloperInstructions,
		Config:                req.Metadata,
	})
	if err != nil {
		return externalagents.ExternalSession{}, err
	}
	return externalagents.ExternalSession{
		AgentID:           AgentID,
		ExternalThreadID:  resp.Thread.ID,
		ExternalSessionID: resp.Thread.SessionID,
		CWD:               resp.CWD,
		Model:             resp.Model,
		ApprovalPolicy:    textutil.FirstNonEmpty(req.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:           textutil.FirstNonEmpty(req.Sandbox, defaultSandbox),
		Metadata:          map[string]any{"mode": "app-server"},
	}, nil
}

func (r *Runtime) ResumeSession(ctx context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	if strings.TrimSpace(session.ExternalThreadID) == "" {
		return externalagents.ExternalSession{}, fmt.Errorf("codexapp: external thread id is required")
	}
	client, err := r.ensureClient(ctx)
	if err != nil {
		return externalagents.ExternalSession{}, err
	}
	resp, err := client.ResumeThread(ctx, ThreadResumeParams{
		ThreadID:       session.ExternalThreadID,
		Model:          session.Model,
		CWD:            session.CWD,
		ApprovalPolicy: textutil.FirstNonEmpty(session.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:        textutil.FirstNonEmpty(session.Sandbox, defaultSandbox),
	})
	if err != nil {
		return externalagents.ExternalSession{}, err
	}
	session.AgentID = AgentID
	session.ExternalThreadID = resp.Thread.ID
	session.ExternalSessionID = resp.Thread.SessionID
	session.CWD = resp.CWD
	session.Model = resp.Model
	session.ApprovalPolicy = textutil.FirstNonEmpty(session.ApprovalPolicy, defaultApprovalPolicy)
	session.Sandbox = textutil.FirstNonEmpty(session.Sandbox, defaultSandbox)
	if session.Metadata == nil {
		session.Metadata = map[string]any{"mode": "app-server"}
	}
	return session, nil
}

func (r *Runtime) Send(ctx context.Context, session externalagents.ExternalSession, input externalagents.Input) (<-chan externalagents.Event, error) {
	if strings.TrimSpace(session.ExternalThreadID) == "" {
		return nil, fmt.Errorf("codexapp: external thread id is required")
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, fmt.Errorf("codexapp: input text is required")
	}
	client, err := r.ensureClient(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.StartTurn(ctx, turnStartParams(session, text))
	if err != nil {
		if !isThreadNotLoadedError(err) {
			return nil, err
		}
		resumed, resumeErr := r.ResumeSession(ctx, session)
		if resumeErr != nil {
			return nil, fmt.Errorf("codexapp: start turn failed: %w; resume failed: %w", err, resumeErr)
		}
		session = resumed
		resp, err = client.StartTurn(ctx, turnStartParams(session, text))
		if err != nil {
			return nil, err
		}
	}

	r.setActiveTurn(session.ExternalThreadID, resp.Turn.ID)
	out := make(chan externalagents.Event, 64)
	safego.Go("codexapp.forwardTurnEvents", func() {
		r.forwardTurnEvents(ctx, out, client, session.ExternalThreadID, resp.Turn.ID)
	})
	return out, nil
}

func turnStartParams(session externalagents.ExternalSession, text string) TurnStartParams {
	return TurnStartParams{
		ThreadID:       session.ExternalThreadID,
		Input:          []UserInput{TextInput(text)},
		ApprovalPolicy: textutil.FirstNonEmpty(session.ApprovalPolicy, defaultApprovalPolicy),
		Model:          strings.TrimSpace(session.Model),
	}
}

func (r *Runtime) Interrupt(ctx context.Context, session externalagents.ExternalSession) error {
	threadID := strings.TrimSpace(session.ExternalThreadID)
	if threadID == "" {
		return fmt.Errorf("codexapp: external thread id is required")
	}
	r.mu.Lock()
	client := r.client
	turnID := r.activeTurns[threadID]
	r.mu.Unlock()
	if client == nil || strings.TrimSpace(turnID) == "" {
		return nil
	}
	return interruptActiveTurn(ctx, client, threadID, turnID)
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client == nil || !r.ownsClient {
		return nil
	}
	err := r.client.Close()
	r.client = nil
	r.initialized = false
	r.activeTurns = map[string]string{}
	return err
}

func (r *Runtime) setActiveTurn(threadID string, turnID string) {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}
	r.mu.Lock()
	if r.activeTurns == nil {
		r.activeTurns = map[string]string{}
	}
	r.activeTurns[threadID] = turnID
	r.mu.Unlock()
}

func (r *Runtime) clearActiveTurn(threadID string, turnID string) {
	r.mu.Lock()
	if r.activeTurns[threadID] == turnID {
		delete(r.activeTurns, threadID)
	}
	r.mu.Unlock()
}

func (r *Runtime) ensureClient(ctx context.Context) (*Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil && r.ownsClient && clientIsDone(r.client) {
		_ = r.client.Close()
		r.client = nil
		r.initialized = false
		r.activeTurns = map[string]string{}
	}
	if r.client == nil {
		// The app-server belongs to the runtime, not to an individual run. Using
		// the run context here kills the shared process as soon as that run
		// finishes and makes the following session inherit a dead client.
		client, err := Start(context.Background(), ProcessOptions{
			Path:   r.path,
			Stderr: r.stderr,
		})
		if err != nil {
			return nil, err
		}
		r.client = client
		r.ownsClient = true
	}
	if !r.initialized {
		// The handshake belongs to the shared process too: abandoning it when
		// one run is canceled leaves codex initialized and refusing a repeat.
		initCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), initializeTimeout)
		defer cancel()
		if _, err := r.client.Initialize(initCtx, InitializeParams{
			ClientInfo: ClientInfo{
				Name:    "matrixclaw",
				Version: "0",
			},
			Capabilities: &InitializeCapabilities{
				ExperimentalAPI: true,
			},
		}); err != nil {
			if r.ownsClient {
				_ = r.client.Close()
				r.client = nil
			}
			return nil, err
		}
		r.initialized = true
	}
	return r.client, nil
}

func clientIsDone(client *Client) bool {
	if client == nil {
		return true
	}
	select {
	case <-client.done:
		return true
	default:
		return false
	}
}

func interruptActiveTurn(ctx context.Context, client *Client, threadID string, turnID string) error {
	if client == nil || strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return nil
	}
	_, err := client.InterruptTurn(ctx, TurnInterruptParams{ThreadID: threadID, TurnID: turnID})
	return err
}

func (r *Runtime) forwardTurnEvents(ctx context.Context, out chan<- externalagents.Event, client *Client, threadID string, turnID string) {
	defer close(out)
	defer func() {
		if ctx.Err() != nil {
			interruptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = interruptActiveTurn(interruptCtx, client, threadID, turnID)
			cancel()
		}
		r.clearActiveTurn(threadID, turnID)
	}()
	if !safego.Run("codexapp.forwardTurnEvents", func() {
		select {
		case out <- externalagents.Event{
			Kind:             externalagents.EventTurnStarted,
			AgentID:          AgentID,
			ExternalThreadID: threadID,
			ExternalTurnID:   turnID,
			At:               time.Now().UTC(),
		}:
		case <-ctx.Done():
			return
		}
		r.forwardTurnEventsLoop(ctx, out, client, threadID, turnID)
	}) {
		sendRuntimeEvent(ctx, out, externalagents.Event{
			Kind:             externalagents.EventTurnFailed,
			AgentID:          AgentID,
			ExternalThreadID: threadID,
			ExternalTurnID:   turnID,
			Error:            "codex app-server event worker panicked",
			At:               time.Now().UTC(),
		})
	}
}

func (r *Runtime) forwardTurnEventsLoop(ctx context.Context, out chan<- externalagents.Event, client *Client, threadID string, turnID string) {
	if client == nil {
		sendRuntimeEvent(ctx, out, externalagents.Event{
			Kind:             externalagents.EventTurnFailed,
			AgentID:          AgentID,
			ExternalThreadID: threadID,
			ExternalTurnID:   turnID,
			Error:            "codex app-server client is unavailable",
			At:               time.Now().UTC(),
		})
		return
	}
	events, unsubscribe := client.SubscribeTurn(ctx, threadID, turnID)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				if err := client.Err(); err != nil {
					sendRuntimeEvent(ctx, out, externalagents.Event{
						Kind:             externalagents.EventTurnFailed,
						AgentID:          AgentID,
						ExternalThreadID: threadID,
						ExternalTurnID:   turnID,
						Error:            err.Error(),
						At:               time.Now().UTC(),
					})
				}
				return
			}
			normalized, done := normalizeNotification(event, threadID, turnID)
			for _, item := range normalized {
				if !sendRuntimeEvent(ctx, out, item) {
					return
				}
			}
			if done {
				return
			}
		}
	}
}

func sendRuntimeEvent(ctx context.Context, out chan<- externalagents.Event, event externalagents.Event) bool {
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func normalizeNotification(event Notification, threadID string, turnID string) ([]externalagents.Event, bool) {
	now := time.Now().UTC()
	switch params := event.Params.(type) {
	case TurnStarted:
		if params.ThreadID != threadID || params.Turn.ID != turnID {
			return nil, false
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventHeartbeat,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.Turn.ID,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case ErrorNotification:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		errText := strings.TrimSpace(params.Error.Message)
		if params.Error.AdditionalDetails != nil && strings.TrimSpace(*params.Error.AdditionalDetails) != "" {
			errText = strings.TrimSpace(errText + ": " + strings.TrimSpace(*params.Error.AdditionalDetails))
		}
		if params.WillRetry {
			return []externalagents.Event{{
				Kind:             externalagents.EventHeartbeat,
				AgentID:          AgentID,
				ExternalThreadID: params.ThreadID,
				ExternalTurnID:   params.TurnID,
				Text:             errText,
				RawMethod:        event.Method,
				Raw:              event.Raw,
				At:               now,
			}}, false
		}
		if errText == "" {
			errText = "codex turn failed"
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventTurnFailed,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			Error:            errText,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, true
	case ContextCompactedNotification:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventHeartbeat,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			Text:             "context compacted",
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case ItemNotification:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		completed := event.Method == "item/completed"
		tool, ok := codexToolFromItem(params.Item, completed)
		if !ok {
			return nil, false
		}
		kind := externalagents.EventToolStarted
		if completed {
			kind = externalagents.EventToolCompleted
		}
		return []externalagents.Event{{
			Kind:             kind,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			ItemID:           tool.ID,
			ToolName:         tool.Name,
			ToolInput:        tool.Input,
			Text:             tool.Output,
			Error:            tool.Error,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case AgentMessageDelta:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventMessageDelta,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			ItemID:           params.ItemID,
			Text:             params.Delta,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case ReasoningTextDelta:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventReasoningDelta,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			ItemID:           params.ItemID,
			Text:             params.Delta,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case ToolOutputDelta:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventToolOutputDelta,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			ItemID:           params.ItemID,
			Text:             params.Delta,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case FileChangePatchUpdated:
		if params.ThreadID != threadID || params.TurnID != turnID {
			return nil, false
		}
		text := formatFileChanges(params.Changes)
		return []externalagents.Event{{
			Kind:             externalagents.EventDiffUpdated,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.TurnID,
			ItemID:           params.ItemID,
			ToolName:         "edit",
			Text:             text,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, false
	case TurnCompleted:
		if params.ThreadID != threadID || params.Turn.ID != turnID {
			return nil, false
		}
		kind, errText := completedTurnOutcome(params.Turn)
		return []externalagents.Event{{
			Kind:             kind,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.Turn.ID,
			Error:            errText,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, true
	default:
		return nil, false
	}
}

func completedTurnOutcome(turn Turn) (externalagents.EventKind, string) {
	switch turn.Status {
	case "", TurnStatusCompleted:
		return externalagents.EventTurnCompleted, ""
	case TurnStatusInterrupted:
		return externalagents.EventTurnFailed, "codex turn interrupted"
	case TurnStatusFailed:
		if turn.Error != nil {
			message := strings.TrimSpace(turn.Error.Message)
			if turn.Error.AdditionalDetails != nil {
				details := strings.TrimSpace(*turn.Error.AdditionalDetails)
				if details != "" && details != message {
					if message != "" {
						message += ": "
					}
					message += details
				}
			}
			if message != "" {
				return externalagents.EventTurnFailed, message
			}
		}
		return externalagents.EventTurnFailed, "codex turn failed"
	default:
		return externalagents.EventTurnFailed, fmt.Sprintf("codex turn completed with unexpected status %q", turn.Status)
	}
}

func isThreadNotLoadedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no rollout found for thread id") || strings.Contains(msg, "thread not found")
}
