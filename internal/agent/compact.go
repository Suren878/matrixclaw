package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// lowYieldLimit is how many summaries in a row may each save under a tenth of
// the prompt before the run stops summarising.
const lowYieldLimit = 2

// compactHistory summarises the history before the newest turns that fit in
// tailPercent of the window into a boundary; reuse, when set, is the step's
// request to ask for the summary with. False means nothing worth summarising:
// no history before the tail, or under a tenth of the prompt.
func (r *run) compactHistory(ctx context.Context, reuse *providers.Request, before int, tailPercent int) (bool, error) {
	previous, messages := r.history.window()
	sent := r.sent(messages)
	limit := r.contextLimit()
	cut := agentcontext.TailStart(sent, limit*tailPercent/100)
	if cut == 0 {
		return false, nil
	}
	covered, coveredTokens := messages[:cut], agentcontext.EstimateMessageTokens(sent[:cut])
	if coveredTokens*10 < before {
		return false, nil
	}
	summary, err := r.summary(ctx, reuse, previous, covered)
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, r.writeBoundary(ctx, previous, covered, summary, before, before-coveredTokens)
}

// summary asks the compact model, when set, or else the step's own request, so
// the provider reuses its cached prefix; without either, or when that fails, the
// run's model summarises the covered history on its own, in chunks.
func (r *run) summary(ctx context.Context, reuse *providers.Request, previous *transcript.Compaction, covered []transcript.Message) (string, error) {
	if r.task.CompactModel != nil || reuse != nil {
		text, err := r.preferredSummary(ctx, reuse, previous, covered)
		if err == nil || ctx.Err() != nil {
			return text, err
		}
	}
	return r.chunkedSummary(ctx, r.task.Model, r.contextLimit()/2, previous, covered)
}

func (r *run) preferredSummary(ctx context.Context, reuse *providers.Request, previous *transcript.Compaction, covered []transcript.Message) (string, error) {
	if r.task.CompactModel != nil {
		limit := ContextLimit(r.task.CompactModel, r.task.CompactWindowTokens, 0)
		return r.chunkedSummary(ctx, r.task.CompactModel, limit/2, previous, covered)
	}
	return r.prefixSummary(ctx, *reuse)
}

// chunkedSummary summarises the covered history with model, outside the run's
// own request, in chunks of about chunkTokens.
func (r *run) chunkedSummary(ctx context.Context, model Model, chunkTokens int, previous *transcript.Compaction, covered []transcript.Message) (string, error) {
	return agentcontext.Summarize(ctx, summaryModel{r: r, model: model}, agentcontext.SummaryInput{
		Previous:    agentcontext.SummaryText(previous),
		Messages:    covered,
		ChunkTokens: chunkTokens,
	})
}

// prefixSummary sends request with the summary instruction appended. Its
// tool_choice stays as it was, since changing it would cost the cached prefix;
// tool calls in the reply are dropped and a reply without text fails.
func (r *run) prefixSummary(ctx context.Context, request providers.Request) (string, error) {
	request.Messages = append(slices.Clone(request.Messages), providers.Message{Role: string(transcript.MessageRoleUser), Content: agentcontext.SummaryInstruction})
	response, err := (summaryModel{r: r, model: r.task.Model}).Generate(ctx, request)
	if err != nil {
		return "", err
	}
	return agentcontext.SummaryReply(response)
}

// writeBoundary journals the boundary whose summary replaces covered and the
// boundary before it; before is the prompt size, rest the part of it that stays.
func (r *run) writeBoundary(ctx context.Context, previous *transcript.Compaction, covered []transcript.Message, summary string, before int, rest int) error {
	compaction := transcript.Compaction{
		Summary:          strings.TrimSpace(summary),
		Kept:             agentcontext.Kept(previous, covered, append([]string{r.task.RunID}, r.task.Continues...)),
		CoversThroughSeq: covered[len(covered)-1].Seq,
		RunID:            r.task.RunID,
		TokensBefore:     before,
	}
	compaction.TokensAfter = max(0, rest-agentcontext.EstimateTextTokens(agentcontext.SummaryText(previous))+agentcontext.EstimateTextTokens(agentcontext.SummaryText(&compaction)))
	r.counters.observeSummary(compaction.TokensBefore, compaction.TokensAfter)
	content := agentcontext.BoundaryLabel(compaction)
	now := r.Now()
	err := r.history.append(ctx, transcript.Message{
		ID:         r.NewID("msg"),
		SessionID:  r.task.SessionID,
		Role:       transcript.MessageRoleSystem,
		Content:    content,
		Parts:      transcript.NormalizeMessageParts(content, nil),
		Compaction: &compaction,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return fmt.Errorf("auto compact session: %w", err)
	}
	r.editedHistory()
	return nil
}

// observeSummary counts the summaries in a row that saved under a tenth of the prompt.
func (c *Counters) observeSummary(before, after int) {
	if before > 0 && (before-after)*10 < before {
		c.LowYield++
		return
	}
	c.LowYield = 0
}
