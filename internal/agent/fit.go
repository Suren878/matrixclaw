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
	output := r.counters.OutputLimit
	if output == 0 {
		output = int(providers.DefaultMaxOutputTokens)
		if limiter, ok := r.task.Model.(providers.OutputLimiter); ok {
			if current, _ := limiter.OutputLimits(); current > 0 {
				output = int(current)
			}
		}
	}
	return agentcontext.EffectiveWindow(r.task.WindowTokens, output)
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
func (r *run) advanceElision() bool {
	_, messages := r.history.window()
	next := agentcontext.NextElision(messages)
	if next.ResultsThroughSeq <= r.counters.ElidedResults && next.ImagesThroughSeq <= r.counters.ElidedImages {
		return false
	}
	r.counters.ElidedResults = max(r.counters.ElidedResults, next.ResultsThroughSeq)
	r.counters.ElidedImages = max(r.counters.ElidedImages, next.ImagesThroughSeq)
	r.anchor = nil
	return true
}

// fitRequest builds the step's request within the model's window: old bulky
// results are elided at 60% of it, older history is summarised at 80%.
func (r *run) fitRequest(ctx context.Context, final StopReason) (providers.Request, error) {
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return providers.Request{}, err
	}
	limit := r.contextLimit()
	tokens := r.promptTokens(request)
	if agentcontext.ElisionDue(tokens, limit) && r.advanceElision() {
		if request, err = r.buildRequest(ctx, final); err != nil {
			return providers.Request{}, err
		}
		tokens = agentcontext.EstimateRequestTokens(request)
	}
	if !agentcontext.SummaryDue(tokens, limit) || r.counters.LowYield >= lowYieldLimit {
		return request, nil
	}
	compacted, err := r.compactHistory(ctx, tokens, agentcontext.TailPercent)
	if err != nil || !compacted {
		return request, err
	}
	if final == "" {
		if err := r.syncContext(ctx); err != nil {
			return providers.Request{}, err
		}
	}
	return r.buildRequest(ctx, final)
}
