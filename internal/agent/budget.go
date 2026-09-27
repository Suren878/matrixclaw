package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// wrapUpShare is the used share of a limit at which the run is told to wrap up.
const wrapUpShare = 0.8

// Counters is what a run has used of its budget, its no-progress streak, its
// output-limit state, its run of low-yield summaries and what its requests elide;
// it is checkpointed so a parked or restarted run continues from it.
type Counters struct {
	Steps         int           `json:"steps,omitempty"`
	Tokens        int64         `json:"tokens,omitempty"`
	Active        time.Duration `json:"active,omitempty"`
	WrapUpSent    bool          `json:"wrap_up_sent,omitempty"`
	LoopHash      string        `json:"loop_hash,omitempty"`
	LoopTool      string        `json:"loop_tool,omitempty"`
	LoopRepeats   int           `json:"loop_repeats,omitempty"`
	LoopWarned    bool          `json:"loop_warned,omitempty"`
	Continuations int           `json:"continuations,omitempty"`
	OutputLimit   int           `json:"output_limit,omitempty"`
	LowYield      int           `json:"low_yield,omitempty"`
	ElidedResults int64         `json:"elided_results,omitempty"`
	ElidedImages  int64         `json:"elided_images,omitempty"`
}

// active is the run's working time: what it had used before plus this Run call.
func (r *run) active() time.Duration {
	return r.task.Resume.Active + r.Now().Sub(r.started)
}

// prepareStep journals the engine notes due before this step's model call and
// returns the stop reason when this step is the tool-less final turn.
func (r *run) prepareStep(ctx context.Context) (StopReason, error) {
	if r.counters.LoopRepeats >= loopStopRepeats {
		return StopLoopDetected, r.appendEngineMessage(ctx, transcript.OriginEngineModel, loopStopText(r.counters.LoopTool))
	}
	if reached := r.exhausted(); reached != "" {
		return StopBudgetExhausted, r.appendEngineMessage(ctx, transcript.OriginEngineModel, budgetStopText(reached))
	}
	if r.counters.LoopRepeats >= loopWarnRepeats && !r.counters.LoopWarned {
		if err := r.appendEngineMessage(ctx, transcript.OriginEngine, loopWarningText(r.counters.LoopTool)); err != nil {
			return "", err
		}
		r.counters.LoopWarned = true
	}
	if !r.counters.WrapUpSent {
		if left := r.remaining(); left != "" {
			if err := r.appendEngineMessage(ctx, transcript.OriginEngine, wrapUpText(left)); err != nil {
				return "", err
			}
			r.counters.WrapUpSent = true
		}
	}
	return "", nil
}

// exhausted describes the first budget limit the run has reached, or "".
func (r *run) exhausted() string {
	b := r.task.Budget
	switch {
	case b.Steps > 0 && r.counters.Steps >= b.Steps:
		return quantity(int64(b.Steps), "step")
	case b.ActiveTime > 0 && r.active() >= b.ActiveTime:
		return "active time of " + approxDuration(b.ActiveTime)
	case b.Tokens > 0 && r.counters.Tokens >= b.Tokens:
		return quantity(b.Tokens, "token")
	default:
		return ""
	}
}

// remaining describes what is left of the most used limit once 80% of it is
// used, or "".
func (r *run) remaining() string {
	b := r.task.Budget
	share, left := 0.0, ""
	consider := func(used float64, text string) {
		if used >= wrapUpShare && used > share {
			share, left = used, text
		}
	}
	if b.Steps > 0 {
		consider(float64(r.counters.Steps)/float64(b.Steps), quantity(int64(b.Steps-r.counters.Steps), "step"))
	}
	if b.ActiveTime > 0 {
		active := r.active()
		consider(float64(active)/float64(b.ActiveTime), approxDuration(b.ActiveTime-active))
	}
	if b.Tokens > 0 {
		consider(float64(r.counters.Tokens)/float64(b.Tokens), quantity(b.Tokens-r.counters.Tokens, "token"))
	}
	return left
}

// finalTurn completes the run with the final turn's text; tool calls a provider
// sent despite tool_choice none are dropped. A context-exhausted final turn
// fails the run with its reply kept.
func finalTurn(gen generation, reason StopReason) stepResult {
	response := gen.response
	response.ToolCalls = nil
	response.Text = sanitizeAssistantOutput(response.Text)
	if response.Text == "" {
		response.Text = finalFallback(reason)
	}
	result := stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: response, stop: reason}
	if reason == StopContextExhausted {
		reply := finalReply(gen.assistant, response)
		result.assistant, result.err, result.markErrored = &reply, ErrContextExhausted, true
	}
	return result
}

// finalFallback is the reply of a final turn that produced no text.
func finalFallback(reason StopReason) string {
	switch reason {
	case StopLoopDetected:
		return "This run stopped: it repeated the same action without progress."
	case StopContextExhausted:
		return "This run stopped: its conversation no longer fits the model's context window."
	default:
		return "This run stopped: it reached its budget."
	}
}

func budgetStopText(reached string) string {
	return "This run has reached its budget (" + reached + "). Do not call tools. Reply briefly: what is done, what remains, and how to continue."
}

func wrapUpText(left string) string {
	return "Budget note: about " + left + " left in this run. Wrap up: finish the current piece of work, verify it and summarise what remains instead of starting something new."
}

func quantity(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// approxDuration renders d to the minute.
func approxDuration(d time.Duration) string {
	minutes := int64(d.Round(time.Minute) / time.Minute)
	switch {
	case minutes < 1:
		return "under a minute"
	case minutes < 60:
		return quantity(minutes, "minute")
	default:
		return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
	}
}
