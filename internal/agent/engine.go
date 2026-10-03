package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Config wires the ports of one run.
type Config struct {
	Journal     Journal
	Tools       Tools
	Approvals   Approvals
	Inbox       Inbox
	Sink        Sink
	Prompts     Prompts
	Todos       Todos
	Attachments agentcontext.AttachmentReader
	Now         func() time.Time
	NewID       func(prefix string) string
	// Sleep waits d or until ctx stops; nil uses a real timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// ModelSlots bounds the model requests of every run sharing it; nil is unbounded.
	ModelSlots *toolsched.Semaphore
	// Locks serialises tool calls sharing a concurrency key across every run
	// using it; nil gives the engine locks of its own.
	Locks *toolsched.Locks
}

// Engine runs the native agent loop over its ports.
type Engine struct {
	cfg Config
}

// New returns an engine over the given ports.
func New(cfg Config) *Engine {
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	if cfg.Locks == nil {
		cfg.Locks = toolsched.NewLocks()
	}
	return &Engine{cfg: cfg}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run executes steps until the run completes, parks, fails or ctx stops. The error
// is reserved for failures that must leave the run's status untouched.
func (e *Engine) Run(ctx context.Context, task Task) (outcome Outcome, err error) {
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
	r := &run{Config: e.cfg, task: task, history: newHistory(e.cfg.Journal, e.cfg.Sink, window), counters: task.Resume, started: e.cfg.Now()}
	defer func() { outcome.Counters = r.counters }()
	if window.Boundary != nil {
		// A boundary edits the history even when no checkpoint recorded it.
		r.counters.HistoryEdit = max(r.counters.HistoryEdit, window.Boundary.Seq)
	}
	r.system = withCustomInstructions(e.cfg.Prompts.System(ctx, window.Messages))
	if ToolUseAllowed(task.Model) {
		r.tools = toolDefinitions(e.cfg.Tools.Specs(ctx))
	}
	for {
		result := r.step(ctx)
		if result.canceled {
			return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, nil
		}
		if ctx.Err() != nil {
			return r.interrupted(result), nil
		}
		settled, done, err := r.settle(ctx, result)
		if err != nil || done {
			return settled, err
		}
	}
}

type run struct {
	Config
	task     Task
	history  *history
	counters Counters
	started  time.Time
	// delegated is the time this Run call only waited for delegated calls.
	delegated  time.Duration
	anchor     *promptAnchor
	requestSeq int64
	// system and tools are built once, so every request of the run shares one
	// prefix.
	system string
	tools  []providers.ToolDefinition
}

type stepKind int

const (
	stepContinue stepKind = iota
	stepWaitingApproval
	stepWaitingEvents
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
	stop        StopReason
}

func (s stepResult) stopReason() StopReason {
	if s.stop == "" {
		return StopDone
	}
	return s.stop
}

func failedStep(err error) stepResult {
	return stepResult{kind: stepDone, err: err}
}

func (r *run) step(ctx context.Context) stepResult {
	waiting, err := r.resumeDecided(ctx)
	if err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingApproval}
	}
	if waiting, err = r.resumeAwait(ctx); err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingEvents}
	}
	if err := r.drainEvents(ctx); err != nil {
		return failedStep(err)
	}
	if err := r.syncContext(ctx); err != nil {
		return failedStep(err)
	}
	final, err := r.prepareStep(ctx)
	if err != nil {
		return failedStep(err)
	}
	request, final, err := r.fitRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
		return failedStep(err)
	}
	r.counters.Steps++
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && providers.IsContextOverflow(err) {
		tokens := r.promptTokens(request)
		r.learnLimit(tokens)
		compacted, compactErr := r.compactHistory(ctx, nil, tokens, agentcontext.TailPercent/2)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if !compacted {
			err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
		} else {
			retry, buildErr := r.afterSummary(ctx, final)
			if buildErr != nil {
				return failedStep(buildErr)
			}
			if gen, err = r.generateWithRetry(ctx, retry); err != nil && providers.IsContextOverflow(err) {
				err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
			}
		}
	}
	if err == nil {
		r.anchorUsage(gen.response)
	}
	if final != "" && errors.Is(err, providers.ErrEmptyResponse) {
		return finalTurn(gen, final)
	}
	if err != nil {
		result := stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: gen.response, err: err, markErrored: true}
		if errors.Is(err, ErrContextExhausted) {
			result.stop = StopContextExhausted
		}
		return result
	}
	if r.canceled(ctx) {
		return stepResult{kind: stepDone, canceled: true, assistant: &gen.assistant, saved: gen.saved}
	}
	if final != "" {
		return finalTurn(gen, final)
	}
	return r.handleResponse(ctx, gen)
}

func (r *run) handleResponse(ctx context.Context, gen generation) stepResult {
	response := gen.response
	response.Text = r.replyText(response)
	assistant := gen.assistant
	if len(response.ToolCalls) > 0 {
		r.counters.Continuations = 0
		if err := r.finishTurn(ctx, &assistant, gen.saved, response, transcript.FinishReasonToolCalls); err != nil {
			return unfinishedTurn(ctx, gen, err)
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
	switch response.StopReason {
	case providers.StopMaxTokens:
		return r.continueCutReply(ctx, gen, response)
	case providers.StopRefusal, providers.StopContentFilter:
		if strings.TrimSpace(response.Text) == "" {
			response.Text = fmt.Sprintf("The provider stopped this reply (%s).", response.StopReason)
		}
		return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response}
	}
	if strings.TrimSpace(response.Text) == "" {
		return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response, err: providers.ErrEmptyResponse, markErrored: true}
	}
	if result, nudged := r.nudgeOpenTodo(ctx, gen, response); nudged {
		return result
	}
	return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response}
}

// replyText cleans a reply's text, keeping the whitespace at an output-limit cut
// on both sides so a cut reply and its continuation join exactly.
func (r *run) replyText(response providers.Response) string {
	text := cleanAssistantOutput(response.Text)
	toolStep := len(response.ToolCalls) > 0
	if toolStep || response.StopReason != providers.StopMaxTokens {
		text = strings.TrimRightFunc(text, unicode.IsSpace)
	}
	if toolStep || r.counters.Continuations == 0 {
		text = strings.TrimLeftFunc(text, unicode.IsSpace)
	}
	return text
}

// finishTurn writes a reply the run goes on after (a tool step, or a reply cut by
// the output limit) with its reasoning, usage and finish reason.
func (r *run) finishTurn(ctx context.Context, assistant *transcript.Message, saved bool, response providers.Response, reason string) error {
	assistant.Content = response.Text
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	assistant.Parts = append(reasoningParts(response), transcript.NormalizeMessageParts(assistant.Content, nil)...)
	finish := usageFinishPart(response.Usage)
	if finish == nil {
		finish = &transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{}}
	}
	finish.Finish.Reason = reason
	assistant.Parts = append(assistant.Parts, *finish)
	assistant.UpdatedAt = r.Now()
	if saved {
		return r.history.finish(ctx, *assistant)
	}
	assistant.CreatedAt = assistant.UpdatedAt
	return r.history.append(ctx, *assistant)
}

// unfinishedTurn fails a step whose turn could not be written; when the run was
// stopped, core seals the streamed preview instead.
func unfinishedTurn(ctx context.Context, gen generation, err error) stepResult {
	if ctx.Err() != nil {
		return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, err: err}
	}
	return failedStep(err)
}

func (r *run) settle(ctx context.Context, result stepResult) (Outcome, bool, error) {
	if result.assistant != nil && r.canceled(ctx) {
		return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, true, nil
	}
	if result.err != nil {
		outcome := Outcome{Status: StatusFailed, Err: result.err, StopReason: result.stop}
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
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingApproval}, true, nil
	case stepWaitingEvents:
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingEvents}, true, nil
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		return Outcome{Status: StatusCompleted, StopReason: result.stopReason(), Assistant: &reply, AssistantSaved: result.saved}, true, nil
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
		outcome.Assistant, outcome.Reached, outcome.StopReason = &reply, StatusCompleted, result.stopReason()
	case stepWaitingApproval:
		outcome.Reached = StatusWaitingApproval
	case stepWaitingEvents:
		outcome.Reached = StatusWaitingEvents
	}
	return outcome
}

func (r *run) canceled(ctx context.Context) bool {
	canceled, err := r.Inbox.Canceled(ctx, r.task.RunID)
	return err == nil && canceled
}

// checkpoint records the durable phase together with the run's counters; only
// the engine goroutine writes it.
func (r *run) checkpoint(ctx context.Context, phase Phase, batch *ToolBatch) error {
	r.counters.Active = r.active()
	return r.Journal.Checkpoint(ctx, State{RunID: r.task.RunID, Phase: phase, Batch: batch, Counters: r.counters})
}
