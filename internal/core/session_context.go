package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return ContextReport{}, err
	}
	return c.contextReportForSession(session, messages), nil
}

// CompactSession summarises the session history into a compaction marker.
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
	}
	if executing, err := c.sessionRunExecuting(ctx, sessionID); err != nil {
		return CompactSessionResult{}, err
	} else if executing {
		return CompactSessionResult{}, fmt.Errorf("%w: wait for the current run to finish before compacting", ErrRunActive)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return CompactSessionResult{}, ErrInvalidInput
	}
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
	content, err := agentcontext.Compact(ctx, runtime, agentcontext.CompactInput{
		SessionID:    session.ID,
		History:      messages,
		BaseTokens:   c.contextBaseTokens(),
		PlanSnapshot: c.compactSessionPlanSnapshot(ctx, session.ID),
	})
	if err != nil {
		return CompactSessionResult{}, err
	}
	message, err := c.CreateSystemMessage(ctx, session.ID, content)
	if err != nil {
		return CompactSessionResult{}, err
	}
	nextMessages, err := c.store.ListMessages(ctx, session.ID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(session, nextMessages)}, nil
}

// sessionRunExecuting reports whether a run of the session is executing in this daemon.
func (c *Core) sessionRunExecuting(ctx context.Context, sessionID string) (bool, error) {
	run, err := c.store.GetActiveRunBySession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return c.runIsActive(run.ID), nil
}

// contextBaseTokens estimates the parts of every request that are not history.
func (c *Core) contextBaseTokens() int {
	assistant := c.assistantProfile()
	return agentcontext.EstimateTextTokens(prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)) +
		agentcontext.EstimateTextTokens(assistant.CustomInstructions) +
		c.estimateToolSchemaTokens()
}

func (c *Core) contextReportForSession(session Session, messages []transcript.Message) ContextReport {
	report := c.contextReport(session.ID, messages)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	recommended, reason := agentcontext.Recommendation(report.TokenEstimate, report.WindowTokens)
	report.Compact = ContextCompact{Recommended: recommended, Reason: reason}
	return report
}

func (c *Core) contextReport(sessionID string, messages []transcript.Message) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	marker := agentcontext.LatestMarker(messages)
	blocks := make([]ContextBlock, 0, 4)
	if systemPrompt != "" {
		blocks = append(blocks, ContextBlock{ID: "system", Kind: ContextBlockSystemPrompt, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(systemPrompt), Included: true, CacheStability: "stable"})
	}
	if customInstructions != "" {
		blocks = append(blocks, ContextBlock{ID: "custom_instructions", Kind: ContextBlockCustomInstructions, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(customInstructions), Included: true, CacheStability: "stable"})
	}
	if marker.Summary != "" {
		block := ContextBlock{ID: "compact_summary", Kind: ContextBlockCompactSummary, Source: "session_compact", TokenEstimate: agentcontext.EstimateTextTokens(marker.Summary), Included: true, CacheStability: "stable"}
		if marker.Cleared {
			block.ID, block.Kind, block.Source = "clear_marker", ContextBlockClearMarker, "session_clear"
		}
		blocks = append(blocks, block)
	}
	if len(marker.Effective) > 0 {
		blocks = append(blocks, ContextBlock{ID: "messages", Kind: ContextBlockMessages, Source: "session_history", TokenEstimate: agentcontext.EstimateMessageTokens(marker.Effective), Included: true, CacheStability: "dynamic"})
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
	recommended, reason := agentcontext.Recommendation(total, 0)
	return ContextReport{
		SessionID:         sessionID,
		Estimated:         true,
		TokenEstimate:     total,
		MessageCount:      len(marker.Effective),
		Blocks:            blocks,
		LastProviderUsage: latestProviderUsage(marker.Effective),
		Compact:           ContextCompact{Recommended: recommended, Reason: reason},
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
