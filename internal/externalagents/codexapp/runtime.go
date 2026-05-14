package codexapp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

type Runtime struct {
	Agent

	mu          sync.Mutex
	client      *Client
	ownsClient  bool
	initialized bool
	stderr      io.Writer
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
)

func NewRuntime(opts RuntimeOptions) *Runtime {
	return &Runtime{
		Agent: Agent{
			Path:    opts.Path,
			Enabled: opts.Enabled,
		},
		client:     opts.Client,
		ownsClient: opts.Client == nil,
		stderr:     opts.Stderr,
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
		ApprovalPolicy:        defaultString(req.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:               defaultString(req.Sandbox, defaultSandbox),
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
		ApprovalPolicy:    defaultString(req.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:           defaultString(req.Sandbox, defaultSandbox),
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
		ApprovalPolicy: defaultString(session.ApprovalPolicy, defaultApprovalPolicy),
		Sandbox:        defaultString(session.Sandbox, defaultSandbox),
	})
	if err != nil {
		return externalagents.ExternalSession{}, err
	}
	session.AgentID = AgentID
	session.ExternalThreadID = resp.Thread.ID
	session.ExternalSessionID = resp.Thread.SessionID
	session.CWD = resp.CWD
	session.Model = resp.Model
	session.ApprovalPolicy = defaultString(session.ApprovalPolicy, defaultApprovalPolicy)
	session.Sandbox = defaultString(session.Sandbox, defaultSandbox)
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
	resp, err := client.StartTurn(ctx, TurnStartParams{
		ThreadID:       session.ExternalThreadID,
		Input:          []UserInput{TextInput(text)},
		ApprovalPolicy: defaultString(session.ApprovalPolicy, defaultApprovalPolicy),
	})
	if err != nil {
		if !isMissingRolloutError(err) {
			return nil, err
		}
		resumed, resumeErr := r.ResumeSession(ctx, session)
		if resumeErr != nil {
			return nil, fmt.Errorf("codexapp: start turn failed: %w; resume failed: %v", err, resumeErr)
		}
		session = resumed
		resp, err = client.StartTurn(ctx, TurnStartParams{
			ThreadID:       session.ExternalThreadID,
			Input:          []UserInput{TextInput(text)},
			ApprovalPolicy: defaultString(session.ApprovalPolicy, defaultApprovalPolicy),
		})
		if err != nil {
			return nil, err
		}
	}

	out := make(chan externalagents.Event, 64)
	go r.forwardTurnEvents(ctx, out, session.ExternalThreadID, resp.Turn.ID)
	return out, nil
}

func (r *Runtime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return fmt.Errorf("codexapp: interrupt is not implemented")
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
	return err
}

func (r *Runtime) ensureClient(ctx context.Context) (*Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client == nil {
		client, err := Start(ctx, ProcessOptions{
			Path:   r.Path,
			Stderr: r.stderr,
		})
		if err != nil {
			return nil, err
		}
		r.client = client
		r.ownsClient = true
	}
	if !r.initialized {
		if _, err := r.client.Initialize(ctx, InitializeParams{
			ClientInfo: ClientInfo{
				Name:    "matrixclaw",
				Version: "0",
			},
			Capabilities: &InitializeCapabilities{
				ExperimentalAPI: true,
			},
		}); err != nil {
			return nil, err
		}
		r.initialized = true
	}
	return r.client, nil
}

func (r *Runtime) forwardTurnEvents(ctx context.Context, out chan<- externalagents.Event, threadID string, turnID string) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			out <- externalagents.Event{
				Kind:             externalagents.EventTurnFailed,
				AgentID:          AgentID,
				ExternalThreadID: threadID,
				ExternalTurnID:   turnID,
				Error:            ctx.Err().Error(),
				At:               time.Now().UTC(),
			}
			return
		case event, ok := <-r.client.Events():
			if !ok {
				if err := r.client.Err(); err != nil {
					out <- externalagents.Event{
						Kind:             externalagents.EventTurnFailed,
						AgentID:          AgentID,
						ExternalThreadID: threadID,
						ExternalTurnID:   turnID,
						Error:            err.Error(),
						At:               time.Now().UTC(),
					}
				}
				return
			}
			normalized, done := normalizeNotification(event, threadID, turnID)
			for _, item := range normalized {
				out <- item
			}
			if done {
				return
			}
		}
	}
}

func normalizeNotification(event Notification, threadID string, turnID string) ([]externalagents.Event, bool) {
	now := time.Now().UTC()
	switch params := event.Params.(type) {
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
	case TurnCompleted:
		if params.ThreadID != threadID || params.Turn.ID != turnID {
			return nil, false
		}
		return []externalagents.Event{{
			Kind:             externalagents.EventTurnCompleted,
			AgentID:          AgentID,
			ExternalThreadID: params.ThreadID,
			ExternalTurnID:   params.Turn.ID,
			RawMethod:        event.Method,
			Raw:              event.Raw,
			At:               now,
		}}, true
	default:
		return nil, false
	}
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func isMissingRolloutError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rollout found for thread id")
}
