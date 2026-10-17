package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// maxContinuations is how many replies in a row the output limit may cut before
// the run's final turn.
const maxContinuations = 3

// unknownCeilingLimit caps a raised output limit when the model's ceiling is
// unknown, so the raise stays within what current models commonly accept.
const unknownCeilingLimit = 32768

const continueText = "Your reply was cut by the output limit. Continue exactly where you stopped, without repeating what you already wrote."

const droppedCallText = "Your last reply hit the output limit, so a tool call it was writing when it was cut was dropped. If you meant to make one, send it again smaller: split a large write into a short file plus edits, or into several files."

// continueCutReply keeps a reply without tool calls cut by the output limit and
// asks the model to go on, with the limit raised once where the model allows it.
// Adapters drop a truncated tool call, so a cut with no text is retried with the
// raised limit, or else the model is told its call was dropped. One cut too many
// makes the next step the run's final turn.
func (r *run) continueCutReply(ctx context.Context, gen generation, response providers.Response) stepResult {
	r.state.Continuations++
	raised := r.raiseOutputLimit()
	if strings.TrimSpace(response.Text) == "" {
		r.state.CallCut = r.state.CallCut || !raised
		return stepResult{kind: stepContinue}
	}
	assistant := gen.assistant
	if err := r.finishTurn(ctx, &assistant, gen.saved, response, transcript.FinishReasonMaxTokens); err != nil {
		return unfinishedTurn(ctx, gen, err)
	}
	if r.outputExhausted() {
		return stepResult{kind: stepContinue}
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, continueText); err != nil {
		return failedStep(err)
	}
	return stepResult{kind: stepContinue}
}

// outputExhausted reports whether the output limit cut more replies in a row than
// the run continues.
func (r *run) outputExhausted() bool {
	return r.state.Continuations > maxContinuations
}

func outputStopText() string {
	return fmt.Sprintf("Your replies were cut by the output limit %d times in a row, so this run stops here. Do not call tools. Reply briefly: what is done, what remains, and how to continue.", maxContinuations+1)
}

// raiseOutputLimit doubles the model's output limit once, capped by its ceiling;
// false when the limit is unknown, already raised or at the cap.
func (r *run) raiseOutputLimit() bool {
	if r.state.OutputLimit > 0 {
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
	r.state.OutputLimit = int(raised)
	return true
}
