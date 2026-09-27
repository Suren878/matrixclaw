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
	sessionID = normalizeText(sessionID)
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
	return c.contextReportForSession(session, window), nil
}

// CompactSession summarises what the model sees of the session into a boundary
// that covers all of it.
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
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
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
	previous := window.Compaction()
	base := c.contextBaseTokens()
	limit := agentcontext.EffectiveWindow(c.sessionContextWindowTokens(session), int(providers.DefaultMaxOutputTokens))
	summary, err := agentcontext.Summarize(ctx, runtime, agentcontext.SummaryInput{
		SessionID:   session.ID,
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
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(session, next)}, nil
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

func (c *Core) contextReportForSession(session Session, window agent.Window) ContextReport {
	report := c.contextReport(session.ID, window)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	recommended, reason := agentcontext.Recommendation(report.TokenEstimate, report.WindowTokens)
	report.Compact = ContextCompact{Recommended: recommended, Reason: reason}
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
	providerID := strings.TrimSpace(session.ProviderID)
	modelID := strings.TrimSpace(session.ModelID)
	providerType := ""
	if llms := c.sessionLLMs(); llms != nil && providerID != "" {
		if manual, ok := llms.(SessionLLMContextWindowRegistry); ok {
			if tokens, found := manual.ContextWindowTokens(providerID, modelID); found && tokens > 0 {
				return tokens
			}
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
