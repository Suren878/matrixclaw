package agent

import (
	"context"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// promptAnchor is the prompt size a provider reported for the run's last request
// and the newest seq that request held.
type promptAnchor struct {
	tokens int
	seq    int64
}

// contextLimit is the prompt room of the run's model: its window without the
// output limit the next request sends and a reserve.
func (r *run) contextLimit() int {
	return agentcontext.EffectiveWindow(r.task.WindowTokens, outputTokens(r.task.Model, r.counters.OutputLimit))
}

// outputTokens is the output limit a request to model sends: limit when set,
// else the model's own, else the default.
func outputTokens(model Model, limit int) int {
	if limit > 0 {
		return limit
	}
	if limiter, ok := model.(providers.OutputLimiter); ok {
		if current, _ := limiter.OutputLimits(); current > 0 {
			return int(current)
		}
	}
	return int(providers.DefaultMaxOutputTokens)
}

// promptTokens is the prompt size of request: the provider's report for the last
// request plus an estimate of what was written since, or an estimate of the
// whole request when no report matches the current prefix.
func (r *run) promptTokens(request providers.Request) int {
	if r.anchor == nil {
		return agentcontext.EstimateRequestTokens(request)
	}
	_, messages := r.history.window()
	tokens := r.anchor.tokens
	for _, message := range messages {
		if message.Seq > r.anchor.seq {
			tokens += agentcontext.EstimateMessageTokens([]transcript.Message{message})
		}
	}
	return tokens
}

// anchorUsage takes the prompt size a main generation reported as the authority
// for the next step.
func (r *run) anchorUsage(response providers.Response) {
	r.anchor = nil
	if response.Usage.PromptTokens > 0 {
		r.anchor = &promptAnchor{tokens: int(response.Usage.PromptTokens), seq: r.requestSeq}
	}
}

// elision is what the run currently hides from its requests.
func (r *run) elision() agentcontext.Elision {
	return agentcontext.Elision{ResultsThroughSeq: r.counters.ElidedResults, ImagesThroughSeq: r.counters.ElidedImages}
}

// advanceElision moves the elision up to the history's current rounds and
// reports whether it moved; the request prefix changes only then.
func (r *run) advanceElision(force bool) bool {
	_, messages := r.history.window()
	next, moved := agentcontext.AdvanceElision(messages, r.elision(), force)
	if !moved {
		return false
	}
	r.counters.ElidedResults, r.counters.ElidedImages = next.ResultsThroughSeq, next.ImagesThroughSeq
	r.editedHistory()
	return true
}

// editedHistory records that the requests' history changed up to its newest
// message: the reported prompt size no longer applies, and reasoning signed
// over the old history is not replayed.
func (r *run) editedHistory() {
	r.anchor = nil
	if all := r.history.all(); len(all) > 0 {
		r.counters.HistoryEdit = all[len(all)-1].Seq
	}
}

const contextExhaustedText = "This run's conversation no longer fits the model's context window, even after summarising it, so the run stops here. Do not call tools. Reply briefly: what is done, what remains, and how to continue."

// fitRequest builds the step's request within the model's window: old bulky
// results are elided at 60% of it and older history is summarised at 80%; once
// summaries stop paying off, the step becomes a context-exhausted final turn.
func (r *run) fitRequest(ctx context.Context, final StopReason) (providers.Request, StopReason, error) {
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return providers.Request{}, final, err
	}
	limit := r.contextLimit()
	tokens := r.promptTokens(request)
	if agentcontext.ElisionDue(tokens, limit) && r.advanceElision(agentcontext.SummaryDue(tokens, limit)) {
		if request, err = r.buildRequest(ctx, final); err != nil {
			return providers.Request{}, final, err
		}
		tokens = agentcontext.EstimateRequestTokens(request)
	}
	if !agentcontext.SummaryDue(tokens, limit) {
		return request, final, nil
	}
	if r.counters.LowYield >= lowYieldLimit {
		return r.exhaustedTurn(ctx, request, final)
	}
	var reuse *providers.Request
	if tokens <= limit {
		reuse = &request
	}
	compacted, err := r.compactHistory(ctx, reuse, tokens, agentcontext.TailPercent)
	if err != nil || !compacted {
		return request, final, err
	}
	request, err = r.afterSummary(ctx, final)
	return request, final, err
}

// afterSummary rebuilds the step's request over the new boundary; a step that is
// not a final turn first re-sends the context note the summary may have covered.
func (r *run) afterSummary(ctx context.Context, final StopReason) (providers.Request, error) {
	if final == "" {
		if err := r.syncContext(ctx); err != nil {
			return providers.Request{}, err
		}
	}
	return r.buildRequest(ctx, final)
}

// exhaustedTurn turns the step into a tool-less final turn that explains the
// context is exhausted; a final turn already under way stays as it is.
func (r *run) exhaustedTurn(ctx context.Context, request providers.Request, final StopReason) (providers.Request, StopReason, error) {
	if final != "" {
		return request, final, nil
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, contextExhaustedText); err != nil {
		return providers.Request{}, final, err
	}
	request, err := r.buildRequest(ctx, StopContextExhausted)
	return request, StopContextExhausted, err
}
