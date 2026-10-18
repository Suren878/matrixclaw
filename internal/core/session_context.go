package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type CompactSessionResult struct {
	Message transcript.Message `json:"message"`
	Context ContextReport      `json:"context"`
}

type ContextBlockKind string

const (
	ContextBlockSystemPrompt       ContextBlockKind = "system_prompt"
	ContextBlockCustomInstructions ContextBlockKind = "custom_instructions"
	ContextBlockCompactSummary     ContextBlockKind = "compact_summary"
	ContextBlockClearMarker        ContextBlockKind = "clear_marker"
	ContextBlockMessages           ContextBlockKind = "messages"
	ContextBlockToolSchemas        ContextBlockKind = "tool_schemas"
)

type ContextBlock struct {
	ID             string           `json:"id"`
	Kind           ContextBlockKind `json:"kind"`
	Source         string           `json:"source"`
	TokenEstimate  int              `json:"token_estimate"`
	Included       bool             `json:"included"`
	Truncated      bool             `json:"truncated,omitempty"`
	CacheStability string           `json:"cache_stability,omitempty"`
}

type ContextReport struct {
	SessionID         string           `json:"session_id"`
	Estimated         bool             `json:"estimated"`
	TokenEstimate     int              `json:"token_estimate"`
	WindowTokens      int              `json:"window_tokens,omitempty"`
	MessageCount      int              `json:"message_count"`
	Blocks            []ContextBlock   `json:"blocks"`
	LastProviderUsage *providers.Usage `json:"last_provider_usage,omitempty"`
	Compact           ContextCompact   `json:"compact"`
}

type ContextCompact struct {
	Recommended bool   `json:"recommended"`
	Reason      string `json:"reason,omitempty"`
}

var ErrSessionRequired = errors.New("session_id is required")

var errNothingToCompact = fmt.Errorf("%w: nothing to compact", ErrInvalidInput)

func (c *Core) SessionContext(ctx context.Context, sessionID string) (ContextReport, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	window, err := c.contextWindow(ctx, sessionID)
	if err != nil {
		return ContextReport{}, err
	}
	return c.contextReportForSession(ctx, session, window), nil
}

// CompactSession summarises what the model sees of the session into a boundary
// that covers all of it, the way a run does when its context fills up; usage
// is recorded on the session's latest run, whose request stays verbatim.
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
	}
	if active, err := c.sessionRunActive(ctx, sessionID); err != nil {
		return CompactSessionResult{}, err
	} else if active {
		return CompactSessionResult{}, fmt.Errorf("%w: finish or cancel the current run before compacting", ErrRunActive)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	session = c.decorateSessionLLM(session)
	window, err := c.contextWindow(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	if len(window.Messages) == 0 {
		return CompactSessionResult{}, errNothingToCompact
	}
	c.preCompact(ctx, session, "manual")
	runtime, model, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
	turn, err := c.sessionTurn(ctx, session, runtime, model)
	if err != nil {
		return CompactSessionResult{}, err
	}
	request := agent.CompactRequest{
		SessionID:    session.ID,
		WorkingDir:   session.WorkingDir,
		Model:        runtime,
		WindowTokens: c.sessionContextWindowTokens(session),
		BaseTokens:   c.contextBase(ctx, turn, window.Messages).tokens(),
	}
	if latest, err := c.store.GetLatestRunBySession(ctx, session.ID); err == nil {
		request.RunID = latest.ID
		if request.Continues, err = c.continuedRuns(ctx, latest); err != nil {
			return CompactSessionResult{}, err
		}
	} else if !errors.Is(err, ErrNotFound) {
		return CompactSessionResult{}, err
	}
	if compact, windowTokens := c.compactRuntime(ctx); compact != nil {
		request.CompactModel, request.CompactWindowTokens = compact, windowTokens
	}
	message, err := c.sessionEngine().Compact(ctx, request)
	if errors.Is(err, agentcontext.ErrNothingToSummarise) {
		return CompactSessionResult{}, errNothingToCompact
	}
	if err != nil {
		return CompactSessionResult{}, err
	}
	next, err := c.contextWindow(ctx, session.ID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(ctx, session, next)}, nil
}

// sessionEngine writes a session's context outside a run: /compact and /clear.
func (c *Core) sessionEngine() *agent.Engine {
	return agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Sink:        coreSink{c: c},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
		ModelSlots:  c.modelSlots,
	})
}

// sessionTurn is the turn a run of the session gets on runtime, named model to
// the model; the caller adds what belongs to the run. On an error the turn is
// not read-only.
func (c *Core) sessionTurn(ctx context.Context, session Session, runtime agent.Model, model string) (nativeTurn, error) {
	readonly, err := c.readonlySubagent(ctx, session.ID)
	if err != nil {
		return nativeTurn{}, err
	}
	_, goal, err := c.activeGoal(ctx, session.ID)
	return nativeTurn{
		SessionID:  session.ID,
		WorkingDir: session.WorkingDir,
		Subagent:   isSubagentSession(session),
		Readonly:   readonly,
		Goal:       goal,
		ToolUse:    agent.ToolUseAllowed(runtime),
		Model:      model,
	}, err
}

// sessionRunActive reports whether the session has an unfinished run, including
// one parked for approval: a boundary would hide the calls it resumes from.
func (c *Core) sessionRunActive(ctx context.Context, sessionID string) (bool, error) {
	_, err := c.store.GetActiveRunBySession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// contextBase is what a run's every request sends besides the history: its
// system prompt, custom instructions and tools.
type contextBase struct {
	system, custom, tools int
}

func (b contextBase) tokens() int {
	return b.system + b.custom + b.tools
}

// contextBase estimates the fixed part of the requests of a run of turn over
// history, from the prompts and tools such a run gets.
func (c *Core) contextBase(ctx context.Context, turn nativeTurn, history []transcript.Message) contextBase {
	system, custom := (&corePrompts{c: c, turn: turn}).System(ctx, history)
	base := contextBase{system: agentcontext.EstimateTextTokens(system), custom: agentcontext.EstimateTextTokens(custom)}
	if turn.ToolUse {
		for _, spec := range (coreTools{c: c, turn: turn}).Specs(ctx) {
			base.tools += agentcontext.EstimateTextTokens(spec.ID) + agentcontext.EstimateTextTokens(spec.Description) + agentcontext.EstimateTextTokens(string(spec.InputJSONSchema))
		}
	}
	return base
}

// contextReportForSession reports the session's context against the prompt room
// its runs get, with the prompts and tools a run would get now; without a
// resolvable model, the default output limit applies.
func (c *Core) contextReportForSession(ctx context.Context, session Session, window agent.Window) ContextReport {
	var model agent.Model
	runtime, label, err := c.resolveSessionRuntime(ctx, session)
	if err == nil {
		model = runtime
	}
	turn, err := c.sessionTurn(ctx, session, model, label)
	if err != nil {
		log.Printf("core: context of session %q: %v", session.ID, err)
	}
	report := c.contextReport(session.ID, c.contextBase(ctx, turn, window.Messages), window)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	limit := agent.ContextLimit(model, report.WindowTokens, 0)
	if agentcontext.SummaryDue(report.TokenEstimate, limit) {
		report.Compact = ContextCompact{Recommended: true, Reason: fmt.Sprintf("estimated context has reached %d%% of the model's usable window; compact before continuing", agentcontext.SummaryPercent)}
	}
	return report
}

func (c *Core) contextReport(sessionID string, base contextBase, window agent.Window) ContextReport {
	blocks := make([]ContextBlock, 0, 5)
	if base.system > 0 {
		blocks = append(blocks, ContextBlock{ID: "system", Kind: ContextBlockSystemPrompt, Source: "run_prompt", TokenEstimate: base.system, Included: true, CacheStability: "stable"})
	}
	if base.custom > 0 {
		blocks = append(blocks, ContextBlock{ID: "custom_instructions", Kind: ContextBlockCustomInstructions, Source: "assistant_profile", TokenEstimate: base.custom, Included: true, CacheStability: "stable"})
	}
	if compaction := window.Compaction(); compaction != nil {
		block := ContextBlock{ID: "compact_summary", Kind: ContextBlockCompactSummary, Source: "session_compact", TokenEstimate: agentcontext.EstimateTextTokens(agentcontext.SummaryText(compaction)), Included: true, CacheStability: "stable"}
		if compaction.Cleared {
			block.ID, block.Kind, block.Source = "clear_marker", ContextBlockClearMarker, "session_clear"
		}
		blocks = append(blocks, block)
	}
	if len(window.Messages) > 0 {
		blocks = append(blocks, ContextBlock{ID: "messages", Kind: ContextBlockMessages, Source: "session_history", TokenEstimate: agentcontext.EstimateMessageTokens(window.Messages), Included: true, CacheStability: "dynamic"})
	}
	if base.tools > 0 {
		blocks = append(blocks, ContextBlock{ID: "tools", Kind: ContextBlockToolSchemas, Source: "tool_registry", TokenEstimate: base.tools, Included: true, CacheStability: "stable"})
	}
	total := 0
	for _, block := range blocks {
		if block.Included {
			total += block.TokenEstimate
		}
	}
	return ContextReport{
		SessionID:         sessionID,
		Estimated:         true,
		TokenEstimate:     total,
		MessageCount:      len(window.Messages),
		Blocks:            blocks,
		LastProviderUsage: latestProviderUsage(window.Messages),
	}
}

func (c *Core) sessionContextWindowTokens(session Session) int {
	session = c.decorateSessionLLM(session)
	return c.modelContextWindowTokens(session.ProviderID, session.ModelID)
}

// DefaultContextWindowCap bounds every model's window unless the daemon
// configures another cap.
const DefaultContextWindowCap = 200_000

// WithContextWindowCap bounds the window runs and /context assume for any
// model; 0 or less keeps the default cap.
func (c *Core) WithContextWindowCap(tokens int) *Core {
	c.windowCap = tokens
	return c
}

// modelContextWindowTokens is the window of a provider's model within the cap:
// the manual setting for the configured model, else the model catalog.
func (c *Core) modelContextWindowTokens(providerID string, modelID string) int {
	windowCap := c.windowCap
	if windowCap <= 0 {
		windowCap = DefaultContextWindowCap
	}
	return min(c.uncappedWindowTokens(providerID, modelID), windowCap)
}

func (c *Core) uncappedWindowTokens(providerID string, modelID string) int {
	if llms := c.sessionLLMs(); llms != nil {
		return llms.ContextWindow(strings.TrimSpace(providerID), strings.TrimSpace(modelID))
	}
	return providers.ResolveContextWindowTokens(providerID, "", modelID)
}

func latestProviderUsage(messages []transcript.Message) *providers.Usage {
	for i := len(messages) - 1; i >= 0; i-- {
		for j := len(messages[i].Parts) - 1; j >= 0; j-- {
			part := messages[i].Parts[j]
			if part.Kind != transcript.MessagePartKindFinish || part.Finish == nil || len(part.Finish.Details) == 0 {
				continue
			}
			var payload struct {
				Usage providers.Usage `json:"usage"`
			}
			if err := json.Unmarshal(part.Finish.Details, &payload); err == nil && !payload.Usage.IsZero() {
				usage := payload.Usage
				return &usage
			}
		}
	}
	return nil
}
