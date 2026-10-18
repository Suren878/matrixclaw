package agent

import (
	"context"
	"errors"
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

// CompactRequest asks Engine.Compact for a summary of everything the model sees
// of a session. RunID, when set, is the run whose steps record the summary's
// usage and whose user messages, with those of Continues, stay verbatim.
type CompactRequest struct {
	SessionID  string
	RunID      string
	Continues  []string
	WorkingDir string
	// Model summarises unless CompactModel is set and succeeds; a window of 0
	// is unknown.
	Model               Model
	WindowTokens        int
	CompactModel        Model
	CompactWindowTokens int
	// BaseTokens estimates what every request sends besides the history.
	BaseTokens int
}

// Compact summarises the session's whole context into a new boundary outside a
// run, the way a run does when its context fills up. ErrNothingToSummarise
// means the context holds nothing a summary could keep.
func (e *Engine) Compact(ctx context.Context, req CompactRequest) (transcript.Message, error) {
	if req.SessionID == "" || req.Model == nil {
		return transcript.Message{}, errors.New("agent: compact requires session and model")
	}
	window, err := e.cfg.Journal.Load(ctx, req.SessionID)
	if err != nil {
		return transcript.Message{}, err
	}
	r := e.newRun(Task{
		RunID:               req.RunID,
		SessionID:           req.SessionID,
		WorkingDir:          req.WorkingDir,
		Model:               req.Model,
		WindowTokens:        req.WindowTokens,
		CompactModel:        req.CompactModel,
		CompactWindowTokens: req.CompactWindowTokens,
		Continues:           req.Continues,
	}, window)
	previous, messages := r.history.window()
	if len(messages) == 0 {
		return transcript.Message{}, agentcontext.ErrNothingToSummarise
	}
	before := agentcontext.SessionTokens(req.BaseTokens, previous, messages)
	return r.compact(ctx, nil, len(messages), before, before-agentcontext.EstimateMessageTokens(messages))
}

// ClearContext starts the session's model context afresh with a boundary that
// covers every message so far.
func (e *Engine) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	window, err := e.cfg.Journal.Load(ctx, sessionID)
	if err != nil {
		return transcript.Message{}, err
	}
	compaction := transcript.Compaction{Cleared: true}
	if window.Boundary != nil {
		compaction.CoversThroughSeq = window.Boundary.Seq
	}
	if n := len(window.Messages); n > 0 {
		compaction.CoversThroughSeq = max(compaction.CoversThroughSeq, window.Messages[n-1].Seq)
	}
	return e.newRun(Task{SessionID: sessionID}, window).appendBoundary(ctx, compaction)
}

// compactHistory summarises the history before the newest turns that fit in
// tailPercent of the window into a boundary; reuse, when set, is the step's
// request to ask for the summary with. False means nothing worth summarising:
// no history before the tail, or under a tenth of the prompt.
func (r *run) compactHistory(ctx context.Context, reuse *providers.Request, before int, tailPercent int) (bool, error) {
	_, messages := r.history.window()
	sent := r.sent(messages)
	cut := agentcontext.TailStart(sent, r.contextLimit()*tailPercent/100)
	if cut == 0 {
		return false, nil
	}
	coveredTokens := agentcontext.EstimateMessageTokens(sent[:cut])
	if coveredTokens*10 < before {
		return false, nil
	}
	if r.Hooks != nil {
		r.Hooks.PreCompact(ctx, "auto")
	}
	if _, err := r.compact(ctx, reuse, cut, before, before-coveredTokens); err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}

// compact is the one path every summary takes, a run's and /compact's: the
// first cut messages of the window become a boundary, which brings a prompt of
// before tokens down to rest plus the new summary; then the files the session
// edited last are re-attached.
func (r *run) compact(ctx context.Context, reuse *providers.Request, cut int, before int, rest int) (transcript.Message, error) {
	previous, messages := r.history.window()
	covered := messages[:cut]
	summary, err := r.summary(ctx, reuse, previous, covered)
	if err != nil {
		return transcript.Message{}, err
	}
	compaction := transcript.Compaction{
		Summary:          strings.TrimSpace(summary),
		Kept:             agentcontext.Kept(previous, covered, r.chain()),
		CoversThroughSeq: covered[len(covered)-1].Seq,
		RunID:            r.task.RunID,
		TokensBefore:     before,
	}
	compaction.TokensAfter = max(0, rest-agentcontext.EstimateTextTokens(agentcontext.SummaryText(previous))+agentcontext.EstimateTextTokens(agentcontext.SummaryText(&compaction)))
	r.state.observeSummary(compaction.TokensBefore, compaction.TokensAfter)
	boundary, err := r.appendBoundary(ctx, compaction)
	if err != nil {
		return transcript.Message{}, err
	}
	return boundary, r.rehydrate(ctx)
}

// chain is the run and the runs it continues; none outside a run.
func (r *run) chain() []string {
	if r.task.RunID == "" {
		return nil
	}
	return append([]string{r.task.RunID}, r.task.Continues...)
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

// appendBoundary journals a context boundary of the session; it is the only
// writer of one.
func (r *run) appendBoundary(ctx context.Context, compaction transcript.Compaction) (transcript.Message, error) {
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
		return transcript.Message{}, err
	}
	r.editedHistory()
	return *r.history.boundary, nil
}

// observeSummary counts the summaries in a row that saved under a tenth of the prompt.
func (c *RunState) observeSummary(before, after int) {
	if before > 0 && (before-after)*10 < before {
		c.LowYield++
		return
	}
	c.LowYield = 0
}
