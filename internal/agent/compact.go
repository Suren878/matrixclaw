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
	limit := r.contextLimit()
	cut := agentcontext.TailStart(messages, limit*tailPercent/100)
	if cut == 0 {
		return false, nil
	}
	covered := messages[:cut]
	if agentcontext.EstimateMessageTokens(covered)*10 < before {
		return false, nil
	}
	summary, err := r.summary(ctx, reuse, previous, covered, limit)
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, r.writeBoundary(ctx, previous, covered, summary, before)
}

// summary asks for the summary with the step's own request, so the provider
// reuses its cached prefix; without one, or when that request is too long, the
// covered history is summarised on its own, in chunks.
func (r *run) summary(ctx context.Context, reuse *providers.Request, previous *transcript.Compaction, covered []transcript.Message, limit int) (string, error) {
	if reuse != nil {
		text, err := r.prefixSummary(ctx, *reuse)
		if err == nil || !agentcontext.IsContextLengthExceeded(err) {
			return text, err
		}
	}
	return agentcontext.Summarize(ctx, summaryModel{r: r}, agentcontext.SummaryInput{
		SessionID:   r.task.SessionID,
		Previous:    agentcontext.SummaryText(previous),
		Messages:    covered,
		ChunkTokens: limit / 2,
	})
}

// prefixSummary sends request with the summary instruction appended and tool
// calls forbidden.
func (r *run) prefixSummary(ctx context.Context, request providers.Request) (string, error) {
	request.Messages = append(slices.Clone(request.Messages), providers.Message{Role: string(transcript.MessageRoleUser), Content: agentcontext.SummaryInstruction})
	request.ToolChoice = providers.ToolChoiceNone
	response, err := summaryModel{r: r}.Generate(ctx, request)
	if err != nil {
		return "", err
	}
	return agentcontext.SummaryReply(response)
}

// writeBoundary journals the boundary whose summary replaces covered and the
// boundary before it.
func (r *run) writeBoundary(ctx context.Context, previous *transcript.Compaction, covered []transcript.Message, summary string, before int) error {
	compaction := transcript.Compaction{
		Summary:          strings.TrimSpace(summary),
		Kept:             agentcontext.Kept(previous, covered, append([]string{r.task.RunID}, r.task.Continues...)),
		CoversThroughSeq: covered[len(covered)-1].Seq,
		RunID:            r.task.RunID,
		TokensBefore:     before,
	}
	compaction.TokensAfter = max(0, before-agentcontext.EstimateMessageTokens(covered)-agentcontext.EstimateTextTokens(agentcontext.SummaryText(previous))+agentcontext.EstimateTextTokens(agentcontext.SummaryText(&compaction)))
	r.counters.observeSummary(compaction.TokensBefore, compaction.TokensAfter)
	r.anchor = nil
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
