package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// maxContinuations is how many replies in a row the output limit may cut before
// the run fails.
const maxContinuations = 3

// unknownCeilingLimit caps a raised output limit when the model's ceiling is
// unknown, so the raise stays within what current models commonly accept.
const unknownCeilingLimit = 32768

const continueText = "Your reply was cut by the output limit. Continue exactly where you stopped, without repeating what you already wrote."

// continueCutReply keeps a reply cut by the output limit and asks the model to go
// on, with the limit raised once where the model allows it: adapters drop a
// truncated tool call, so a cut may hide one. A cut before any text is retried
// only when the limit can still be raised.
func (r *run) continueCutReply(ctx context.Context, gen generation, response providers.Response) stepResult {
	if response.Text == "" {
		if !r.raiseOutputLimit() {
			return failedStep(errors.New("reply cut by the output limit before any text"))
		}
		return stepResult{kind: stepContinue}
	}
	if r.counters.Continuations >= maxContinuations {
		reply := finalReply(gen.assistant, response)
		return stepResult{kind: stepDone, assistant: &reply, saved: gen.saved, response: response, err: fmt.Errorf("reply cut by the output limit %d times in a row", maxContinuations+1), markErrored: true}
	}
	r.raiseOutputLimit()
	r.counters.Continuations++
	assistant := gen.assistant
	if err := r.finishTurn(ctx, &assistant, gen.saved, response, string(providers.StopMaxTokens)); err != nil {
		return unfinishedTurn(ctx, gen, err)
	}
	if err := r.appendEngineMessage(ctx, continueText); err != nil {
		return failedStep(err)
	}
	return stepResult{kind: stepContinue}
}

// raiseOutputLimit doubles the model's output limit once, capped by its ceiling;
// false when the limit is unknown, already raised or at the cap.
func (r *run) raiseOutputLimit() bool {
	if r.counters.OutputLimit > 0 {
		return false
	}
	limiter, ok := r.task.Model.(providers.OutputLimiter)
	if !ok {
		return false
	}
	current, ceiling := limiter.OutputLimits()
	if ceiling <= 0 {
		ceiling = unknownCeilingLimit
	}
	raised := min(current*2, ceiling)
	if raised <= current {
		return false
	}
	r.counters.OutputLimit = int(raised)
	return true
}
