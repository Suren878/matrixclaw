package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ProgressFlushInterval is how often a streaming reply is written to the journal.
const ProgressFlushInterval = 750 * time.Millisecond

var errRunCanceled = errors.New("run canceled")

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
func (r *run) recordStep(ctx context.Context, response providers.Response, stopReason string, latency time.Duration) error {
	r.counters.Tokens += response.Usage.PromptTokens + response.Usage.OutputTokens
	return r.Journal.RecordStep(ctx, Step{
		RunID:      r.task.RunID,
		Model:      response.Model,
		Provider:   response.Provider,
		Usage:      response.Usage,
		StopReason: stopReason,
		Latency:    latency,
		ToolCalls:  len(response.ToolCalls),
	})
}

// retryBackoffs are the waits before each retry of a failed generation.
var retryBackoffs = [...]time.Duration{200 * time.Millisecond, 750 * time.Millisecond}

// maxRetryAfter caps the wait a provider may ask for; a longer one fails the
// generation instead of stalling the run.
const maxRetryAfter = time.Minute

// retryDelay is the wait before retrying err on attempt, honouring the
// provider's Retry-After; false means err is not retried.
func retryDelay(err error, attempt int) (time.Duration, bool) {
	if attempt >= len(retryBackoffs) || !providers.IsRetryableGenerationError(err) {
		return 0, false
	}
	delay := retryBackoffs[attempt]
	var apiErr *providers.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > delay {
		if apiErr.RetryAfter > maxRetryAfter {
			return 0, false
		}
		delay = apiErr.RetryAfter
	}
	return delay, true
}

// summaryModel generates summaries with model and records every successful one
// as a compact step of the run; a transient failure is retried like a main
// generation.
type summaryModel struct {
	r     *run
	model Model
}

func (m summaryModel) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	for attempt := 0; ; attempt++ {
		response, latency, err := m.r.generateOnSlot(ctx, m.model, request)
		if err == nil {
			return response, m.r.recordStep(ctx, response, CompactStopReason, latency)
		}
		delay, retry := retryDelay(err, attempt)
		if ctx.Err() != nil || !retry {
			return response, err
		}
		if err := m.r.Sleep(ctx, delay); err != nil {
			return response, err
		}
	}
}

// generateWithRetry retries only failures that happened before any output was shown;
// a partial answer stays visible as failed instead of being replayed. An empty
// final turn is not retried: it falls back to the stop note.
func (r *run) generateWithRetry(ctx context.Context, request providers.Request) (generation, error) {
	for attempt := 0; ; attempt++ {
		gen, err := r.generate(ctx, request)
		if err == nil && sanitizeAssistantOutput(gen.response.Text) == "" && len(gen.response.ToolCalls) == 0 && !gen.response.StopReason.AllowsEmptyReply() {
			err = providers.ErrEmptyResponse
		}
		if request.ToolChoice == providers.ToolChoiceNone && errors.Is(err, providers.ErrEmptyResponse) {
			return gen, err
		}
		if err == nil || gen.saved || gen.assistant.Content != "" || ctx.Err() != nil {
			return gen, err
		}
		delay, retry := retryDelay(err, attempt)
		if !retry {
			return gen, err
		}
		if err := r.Sleep(ctx, delay); err != nil {
			return gen, err
		}
	}
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
	var lastFlush, lastCancelCheck time.Time
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
	sanitizer := newAssistantStreamSanitizer()
	streamCtx := providers.WithTextStream(ctx, func(delta string) error {
		select {
		case <-ctx.Done():
			return errRunCanceled
		default:
		}
		now := r.Now()
		if lastCancelCheck.IsZero() || now.Sub(lastCancelCheck) >= ProgressFlushInterval {
			lastCancelCheck = now
			if r.canceled(ctx) {
				return errRunCanceled
			}
		}
		if !gen.saved && gen.assistant.Content == "" {
			delta = strings.TrimPrefix(delta, "\n")
		}
		if delta = sanitizer.Push(delta); delta == "" {
			return nil
		}
		gen.assistant.Content += delta
		dirty = true
		return flush(false)
	})
	response, latency, err := r.generateOnSlot(streamCtx, r.task.Model, request)
	if err == nil {
		err = r.recordStep(ctx, response, stepStopReason(response), latency)
	}
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
