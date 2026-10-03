package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/agent/prompt"
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
// that covers all of it.
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
	runtime, windowTokens := c.compactRuntime(ctx)
	if runtime == nil {
		if runtime, _, err = c.resolveSessionRuntime(ctx, session); err != nil {
			return CompactSessionResult{}, err
		}
		windowTokens = c.sessionContextWindowTokens(session)
	}
	previous := window.Compaction()
	base := c.contextBaseTokens()
	limit := agent.ContextLimit(runtime, windowTokens, 0)
	summary, err := agentcontext.Summarize(ctx, runtime, agentcontext.SummaryInput{
		Previous:    agentcontext.SummaryText(previous),
		Messages:    window.Messages,
		ChunkTokens: limit / 2,
	})
	if errors.Is(err, agentcontext.ErrNothingToSummarise) {
		return CompactSessionResult{}, errNothingToCompact
	}
	if err != nil {
		return CompactSessionResult{}, err
	}
	compaction := transcript.Compaction{
		Summary:          summary,
		CoversThroughSeq: window.Messages[len(window.Messages)-1].Seq,
		TokensBefore:     agentcontext.SessionTokens(base, previous, window.Messages),
	}
	compaction.TokensAfter = agentcontext.SessionTokens(base, &compaction, nil)
	message, err := c.appendBoundary(ctx, session.ID, agentcontext.BoundaryLabel(compaction), compaction)
	if err != nil {
		return CompactSessionResult{}, err
	}
	next, err := c.contextWindow(ctx, session.ID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(ctx, session, next)}, nil
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

// contextBaseTokens estimates the parts of every request that are not history.
func (c *Core) contextBaseTokens() int {
	assistant := c.assistantProfile()
	return agentcontext.EstimateTextTokens(prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)) +
		agentcontext.EstimateTextTokens(assistant.CustomInstructions) +
		c.estimateToolSchemaTokens()
}

// contextReportForSession reports the session's context against the prompt room
// its runs get; without a resolvable model, the default output limit applies.
func (c *Core) contextReportForSession(ctx context.Context, session Session, window agent.Window) ContextReport {
	report := c.contextReport(session.ID, window)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	var model agent.Model
	if runtime, _, err := c.resolveSessionRuntime(ctx, session); err == nil {
		model = runtime
	}
	limit := agent.ContextLimit(model, report.WindowTokens, 0)
	if agentcontext.SummaryDue(report.TokenEstimate, limit) {
		report.Compact = ContextCompact{Recommended: true, Reason: fmt.Sprintf("estimated context has reached %d%% of the model's usable window; compact before continuing", agentcontext.SummaryPercent)}
	}
	return report
}

func (c *Core) contextReport(sessionID string, window agent.Window) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	blocks := make([]ContextBlock, 0, 5)
	if systemPrompt != "" {
		blocks = append(blocks, ContextBlock{ID: "system", Kind: ContextBlockSystemPrompt, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(systemPrompt), Included: true, CacheStability: "stable"})
	}
	if customInstructions != "" {
		blocks = append(blocks, ContextBlock{ID: "custom_instructions", Kind: ContextBlockCustomInstructions, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(customInstructions), Included: true, CacheStability: "stable"})
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
	if estimate := c.estimateToolSchemaTokens(); estimate > 0 {
		blocks = append(blocks, ContextBlock{ID: "tools", Kind: ContextBlockToolSchemas, Source: "tool_registry", TokenEstimate: estimate, Included: true, CacheStability: "stable"})
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
// a manual setting first, then the model catalog, else the fallback.
func (c *Core) modelContextWindowTokens(providerID string, modelID string) int {
	windowCap := c.windowCap
	if windowCap <= 0 {
		windowCap = DefaultContextWindowCap
	}
	return min(c.uncappedWindowTokens(providerID, modelID), windowCap)
}

func (c *Core) uncappedWindowTokens(providerID string, modelID string) int {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	providerType := ""
	if llms := c.sessionLLMs(); llms != nil && providerID != "" {
		if tokens, found := llms.ContextWindowTokens(providerID, modelID); found && tokens > 0 {
			return tokens
		}
		if option, resolved, err := llms.Normalize(providerID, modelID); err == nil {
			providerType = option.Type
			if strings.TrimSpace(resolved) != "" {
				modelID = resolved
			}
		}
	}
	return providers.ResolveContextWindowTokens(providerID, providerType, modelID)
}

func (c *Core) estimateToolSchemaTokens() int {
	if c.tools == nil {
		return 0
	}
	total := 0
	for _, tool := range c.tools.List() {
		total += agentcontext.EstimateTextTokens(tool.ID)
		total += agentcontext.EstimateTextTokens(tool.Description)
		total += agentcontext.EstimateTextTokens(string(tool.InputJSONSchema))
	}
	return total
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
