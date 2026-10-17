package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ProgressFlushInterval is how often a streaming reply is written to the journal.
const ProgressFlushInterval = 750 * time.Millisecond

// generation is one model call and the assistant message it streamed.
type generation struct {
	assistant transcript.Message
	saved     bool
	response  providers.Response
}

// CompactStopReason marks summary generations in run_steps; they use no budget step.
const CompactStopReason = "compact"

// recordStep stores one successful generation as a run step and counts its tokens
// against the run's budget.
func (r *run) recordStep(ctx context.Context, response providers.Response, stopReason string, latency time.Duration) {
	r.state.Tokens += response.Usage.PromptTokens + response.Usage.OutputTokens
	r.Journal.RecordStep(ctx, Step{
		RunID:      r.task.RunID,
		Model:      response.Model,
		Provider:   response.Provider,
		Usage:      response.Usage,
		StopReason: stopReason,
		Latency:    latency,
		ToolCalls:  len(response.ToolCalls),
	})
}

// A failed generation is tried up to maxAttempts times. The waits double from
// retryBase, or from limitBase for a rate-limited or overloaded provider that
// gave no Retry-After, with jitter; together they stay within maxRetryWait.
const (
	maxAttempts  = 6
	maxRetryWait = 2 * time.Minute
	retryBase    = time.Second
	limitBase    = 2 * time.Second
)

// retryDelay is the wait before retrying err after attempt failed attempts that
// already waited waited; false means err is not retried. A malformed tool call
// goes back to the model instead.
func retryDelay(err error, attempt int, waited time.Duration) (time.Duration, bool) {
	if attempt+1 >= maxAttempts || errors.Is(err, providers.ErrMalformedToolCall) || !providers.IsRetryableGenerationError(err) {
		return 0, false
	}
	base := retryBase
	var apiErr *providers.APIError
	limited := errors.As(err, &apiErr) && apiErr.Retryable()
	if limited {
		base = limitBase
	}
	backoff := base << attempt
	delay := backoff/2 + rand.N(backoff/2+1)
	if limited && apiErr.RetryAfter > 0 {
		delay = apiErr.RetryAfter
	}
	if waited+delay > maxRetryWait {
		return 0, false
	}
	return delay, true
}

// withRetry runs attempt until it succeeds or fails for good. Before each wait
// settle, when set, cleans up after the failed attempt and may decline the retry.
func (r *run) withRetry(ctx context.Context, attempt func() error, settle func(error) (bool, error)) error {
	var waited time.Duration
	for n := 0; ; n++ {
		err := attempt()
		delay, retry := retryDelay(err, n, waited)
		if err == nil || !retry || ctx.Err() != nil {
			return err
		}
		if settle != nil {
			again, settleErr := settle(err)
			if settleErr != nil {
				return errors.Join(err, settleErr)
			}
			if !again {
				return err
			}
		}
		if err := r.Sleep(ctx, delay); err != nil {
			return err
		}
		waited += delay
	}
}

// summaryModel generates summaries with model and records every successful one
// as a compact step of the run; a transient failure is retried like a main
// generation.
type summaryModel struct {
	r     *run
	model Model
}

func (m summaryModel) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	var response providers.Response
	err := m.r.withRetry(ctx, func() error {
		var latency time.Duration
		var err error
		response, latency, err = m.r.generateOnSlot(ctx, m.model, request)
		if err == nil {
			m.r.recordStep(ctx, response, CompactStopReason, latency)
		}
		return err
	}, nil)
	return response, err
}

// generateWithRetry generates the step's reply, retrying transient failures. A
// reply that failed while streaming is sealed out of the conversation first, so
// the model never reads it. An empty final turn is not retried: it falls back
// to the stop note.
func (r *run) generateWithRetry(ctx context.Context, request providers.Request) (generation, error) {
	var gen generation
	err := r.withRetry(ctx, func() error {
		var err error
		gen, err = r.generate(ctx, request)
		if err == nil && sanitizeAssistantOutput(gen.response.Text) == "" && len(gen.response.ToolCalls) == 0 && !gen.response.StopReason.AllowsEmptyReply() {
			err = providers.ErrEmptyResponse
		}
		return err
	}, func(err error) (bool, error) {
		if request.ToolChoice == providers.ToolChoiceNone && errors.Is(err, providers.ErrEmptyResponse) {
			return false, nil
		}
		return true, r.dropPartial(ctx, gen, fmt.Sprintf("Generation failed (%v); the reply was generated again.", err))
	})
	return gen, err
}

// dropPartial seals a reply that stopped while streaming with a note, so clients
// keep it but no request replays it.
func (r *run) dropPartial(ctx context.Context, gen generation, note string) error {
	if !gen.saved {
		return nil
	}
	return r.sealDropped(ctx, gen.assistant, note)
}

// sealDropped finishes message with a note that drops it from every request.
func (r *run) sealDropped(ctx context.Context, message transcript.Message, note string) error {
	message.Parts = append(transcript.NormalizeMessageParts(message.Content, message.Parts), transcript.MessagePart{
		Kind:   transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{Reason: transcript.FinishReasonDaemonRestart, Message: note},
	})
	message.UpdatedAt = r.Now()
	return r.history.finish(ctx, message)
}

// maxMalformed is how many replies in a row may carry a tool call the adapter
// could not decode before the run fails.
const maxMalformed = 3

// malformedReply hands a reply whose tool call could not be decoded back to the
// model: the streamed text is dropped and the model is told what was wrong.
func (r *run) malformedReply(ctx context.Context, gen generation, err error) stepResult {
	r.state.Malformed++
	if err := r.dropPartial(ctx, gen, "This reply carried a malformed tool call; the model was asked to send it again."); err != nil {
		return failedStep(err)
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, malformedCallText(err)); err != nil {
		return failedStep(err)
	}
	return stepResult{kind: stepContinue}
}

func malformedCallText(err error) string {
	return "Your last reply was dropped: a tool call in it could not be decoded (" + err.Error() + "). Send the call again with a complete JSON object as its arguments; split very large content across several calls."
}

// generate streams one assistant turn, writing progress at most every flush interval.
func (r *run) generate(ctx context.Context, request providers.Request) (generation, error) {
	gen := generation{assistant: transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		RunID:     r.task.RunID,
		Role:      transcript.MessageRoleAssistant,
	}}
	dirty := false
	var lastFlush time.Time
	flush := func(force bool) error {
		if !dirty {
			return nil
		}
		now := r.Now()
		if !force && gen.saved && !lastFlush.IsZero() && now.Sub(lastFlush) < ProgressFlushInterval {
			return nil
		}
		gen.assistant.Parts = transcript.NormalizeMessageParts(gen.assistant.Content, nil)
		gen.assistant.UpdatedAt = now
		if gen.saved {
			if err := r.history.stream(ctx, gen.assistant); err != nil {
				return err
			}
		} else {
			gen.assistant.CreatedAt = now
			if err := r.history.beginStreaming(ctx, gen.assistant); err != nil {
				return err
			}
			gen.saved = true
		}
		dirty = false
		lastFlush = now
		return nil
	}
	var sanitizer replySanitizer
	add := func(text string) {
		if text != "" {
			gen.assistant.Content += text
			dirty = true
		}
	}
	streamCtx := providers.WithTextStream(ctx, func(delta string) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		add(sanitizer.Push(delta))
		return flush(false)
	})
	response, latency, err := r.generateOnSlot(streamCtx, r.task.Model, request)
	if err == nil {
		r.recordStep(ctx, response, stepStopReason(response), latency)
	}
	add(sanitizer.Flush())
	if flushErr := flush(true); flushErr != nil {
		err = errors.Join(err, flushErr)
	}
	gen.response = response
	return gen, err
}

// generateOnSlot runs one generation holding a model slot, given back even if
// the model panics; the latency leaves out the wait for the slot.
func (r *run) generateOnSlot(ctx context.Context, model Model, request providers.Request) (providers.Response, time.Duration, error) {
	release, err := r.ModelSlots.Acquire(ctx)
	if err != nil {
		return providers.Response{}, 0, err
	}
	defer release()
	started := time.Now()
	response, err := model.Generate(ctx, request)
	return response, time.Since(started), err
}

// stepStopReason is the run_steps stop reason of a model generation.
func stepStopReason(response providers.Response) string {
	return string(providers.ResolveStopReason(response.StopReason, len(response.ToolCalls)))
}
