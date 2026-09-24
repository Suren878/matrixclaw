package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const maxSteps = 32

// Config wires the ports of one run.
type Config struct {
	Journal     Journal
	Tools       Tools
	Approvals   Approvals
	Inbox       Inbox
	Sink        Sink
	Prompts     Prompts
	Attachments agentcontext.AttachmentReader
	Now         func() time.Time
	NewID       func(prefix string) string
}

// Engine runs the native agent loop over its ports.
type Engine struct {
	cfg Config
}

// New returns an engine over the given ports.
func New(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}

// Run executes steps until the run completes, parks, fails or ctx stops. The error
// is reserved for failures that must leave the run's status untouched.
func (e *Engine) Run(ctx context.Context, task Task) (Outcome, error) {
	if task.RunID == "" || task.SessionID == "" || task.Model == nil {
		return Outcome{}, errors.New("agent: task requires run, session and model")
	}
	window, err := e.cfg.Journal.Load(ctx, task.SessionID)
	if err != nil {
		if ctx.Err() != nil {
			return Outcome{Status: StatusInterrupted}, nil
		}
		return Outcome{Status: StatusFailed, Err: err}, nil
	}
	r := &run{Config: e.cfg, task: task, history: newHistory(e.cfg.Journal, e.cfg.Sink, window)}
	for step := 0; step < maxSteps; step++ {
		result := r.step(ctx)
		if result.canceled {
			return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, nil
		}
		if ctx.Err() != nil {
			return r.interrupted(result), nil
		}
		outcome, done, err := r.settle(ctx, result)
		if err != nil || done {
			return outcome, err
		}
	}
	return Outcome{Status: StatusFailed, Err: fmt.Errorf("tool loop exceeded %d steps", maxSteps)}, nil
}

type run struct {
	Config
	task    Task
	history *history
}

type stepKind int

const (
	stepContinue stepKind = iota
	stepWaitingApproval
	stepDone
)

type stepResult struct {
	kind        stepKind
	canceled    bool
	assistant   *transcript.Message
	saved       bool
	response    providers.Response
	err         error
	markErrored bool
}

func failedStep(err error) stepResult {
	return stepResult{kind: stepDone, err: err}
}

func (r *run) step(ctx context.Context) stepResult {
	waiting, err := r.resumeApproved(ctx)
	if err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingApproval}
	}
	if err := r.checkpoint(ctx, PhaseModel, "", ""); err != nil {
		return failedStep(err)
	}
	compacted, err := r.autoCompact(ctx)
	if err != nil {
		return failedStep(err)
	}
	request, err := r.buildRequest(ctx)
	if err != nil {
		return failedStep(err)
	}
	if !compacted && r.requestNeedsCompact(ctx, request) {
		if compacted, err = r.compact(ctx); err != nil {
			return failedStep(err)
		}
		if compacted {
			if request, err = r.buildRequest(ctx); err != nil {
				return failedStep(err)
			}
		}
	}
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		compacted, compactErr := r.compact(ctx)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if compacted {
			retry, buildErr := r.buildRequest(ctx)
			if buildErr != nil {
				return failedStep(buildErr)
			}
			gen, err = r.generateWithRetry(ctx, retry)
		}
	}
	if err != nil {
		return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: gen.response, err: err, markErrored: true}
	}
	if r.canceled(ctx) {
		return stepResult{kind: stepDone, canceled: true, assistant: &gen.assistant, saved: gen.saved}
	}
	return r.handleResponse(ctx, gen)
}

func (r *run) handleResponse(ctx context.Context, gen generation) stepResult {
	response := gen.response
	response.Text = sanitizeAssistantOutput(response.Text)
	assistant := gen.assistant
	if len(response.ToolCalls) > 0 {
		if err := r.finishToolTurn(ctx, &assistant, gen.saved, response); err != nil {
			if ctx.Err() != nil {
				// Stopped before the tool turn was written: core seals the streamed preview.
				return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, err: err}
			}
			return failedStep(err)
		}
		waiting, err := r.executeBatch(ctx, response)
		if err != nil {
			return stepResult{kind: stepDone, assistant: &assistant, saved: true, response: response, err: err}
		}
		if waiting {
			return stepResult{kind: stepWaitingApproval}
		}
		return stepResult{kind: stepContinue}
	}
	if strings.TrimSpace(response.Text) == "" {
		return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response, err: providers.ErrEmptyResponse, markErrored: true}
	}
	return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response}
}

// finishToolTurn writes the model's commentary, reasoning and usage before its tools run.
func (r *run) finishToolTurn(ctx context.Context, assistant *transcript.Message, saved bool, response providers.Response) error {
	assistant.Content = response.Text
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	assistant.Parts = append(reasoningParts(response), transcript.NormalizeMessageParts(assistant.Content, nil)...)
	finish := usageFinishPart(response.Usage)
	if finish == nil {
		finish = &transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{}}
	}
	finish.Finish.Reason = "tool_calls"
	assistant.Parts = append(assistant.Parts, *finish)
	assistant.UpdatedAt = r.Now()
	if saved {
		return r.history.finish(ctx, *assistant)
	}
	assistant.CreatedAt = assistant.UpdatedAt
	return r.history.append(ctx, *assistant)
}

func (r *run) settle(ctx context.Context, result stepResult) (Outcome, bool, error) {
	if result.assistant != nil && r.canceled(ctx) {
		return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, true, nil
	}
	if result.err != nil {
		outcome := Outcome{Status: StatusFailed, Err: result.err}
		if result.markErrored && result.assistant != nil {
			outcome.Assistant, outcome.AssistantSaved, outcome.MarkErrored = result.assistant, result.saved, true
		}
		return outcome, true, nil
	}
	switch result.kind {
	case stepWaitingApproval:
		pending, err := r.Approvals.Pending(ctx, r.task.RunID)
		if err != nil {
			return Outcome{}, true, err
		}
		if !pending {
			return Outcome{}, false, nil
		}
		return Outcome{Status: StatusWaitingApproval}, true, nil
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		return Outcome{Status: StatusCompleted, Assistant: &reply, AssistantSaved: result.saved}, true, nil
	default:
		return Outcome{}, false, nil
	}
}

func (r *run) interrupted(result stepResult) Outcome {
	outcome := Outcome{Status: StatusInterrupted, Assistant: result.assistant, AssistantSaved: result.saved}
	if result.err != nil {
		return outcome
	}
	switch result.kind {
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		outcome.Assistant, outcome.Reached = &reply, StatusCompleted
	case stepWaitingApproval:
		outcome.Reached = StatusWaitingApproval
	}
	return outcome
}

func (r *run) canceled(ctx context.Context) bool {
	canceled, err := r.Inbox.Canceled(ctx, r.task.RunID)
	return err == nil && canceled
}

func (r *run) checkpoint(ctx context.Context, phase Phase, toolCallID string, toolName string) error {
	return r.Journal.Checkpoint(ctx, State{RunID: r.task.RunID, Phase: phase, ToolCallID: toolCallID, ToolName: toolName})
}
