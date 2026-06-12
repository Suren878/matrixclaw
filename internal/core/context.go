package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const compactMessagePrefix = "🧠 Context compacted"
const contextClearedMessagePrefix = "🧹 Context cleared"
const contextClearedSummary = "Context cleared by user."
const maxCompactPromptRunes = 80_000
const EstimatedImageTokens = 1_500
const compactBackoffMinimumSavingsPercent = 10

type CompactSessionResult struct {
	Message Message       `json:"message"`
	Context ContextReport `json:"context"`
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
	SessionID         string         `json:"session_id"`
	Estimated         bool           `json:"estimated"`
	TokenEstimate     int            `json:"token_estimate"`
	WindowTokens      int            `json:"window_tokens,omitempty"`
	MessageCount      int            `json:"message_count"`
	Blocks            []ContextBlock `json:"blocks"`
	LastProviderUsage *ProviderUsage `json:"last_provider_usage,omitempty"`
	Compact           ContextCompact `json:"compact"`
}

type ProviderUsage struct {
	InputTokens     int64           `json:"input_tokens,omitempty"`
	OutputTokens    int64           `json:"output_tokens,omitempty"`
	TotalTokens     int64           `json:"total_tokens,omitempty"`
	CachedTokens    int64           `json:"cached_tokens,omitempty"`
	ReasoningTokens int64           `json:"reasoning_tokens,omitempty"`
	Estimated       bool            `json:"estimated,omitempty"`
	ProviderRaw     json.RawMessage `json:"provider_raw,omitempty"`
}

type ContextCompact struct {
	Recommended bool   `json:"recommended"`
	Reason      string `json:"reason,omitempty"`
}

var ErrSessionRequired = errors.New("session_id is required")

func ContextClearedMessageContent() string {
	return contextClearedMessagePrefix + "\n\n" + contextClearedSummary
}

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

func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
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
	before := c.contextReportForSession(session, messages)
	_, effectiveMessages := latestCompactSummary(messages)
	if len(effectiveMessages) == 0 {
		return CompactSessionResult{}, ErrInvalidInput
	}

	summary, err := c.generateCompactSummary(ctx, session, effectiveMessages)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summary = strings.TrimSpace(summary)
	previewContent := compactMessagePrefix + "\n\n" + summary
	preview := Message{
		Role:    MessageRoleSystem,
		Content: previewContent,
		Parts: []MessagePart{{
			Kind: MessagePartKindText,
			Text: &TextPart{Text: previewContent},
		}},
	}
	after := c.contextReportForSession(session, append(append([]Message(nil), messages...), preview))
	content := fmt.Sprintf("%s: ~%s -> ~%s tokens\n\n%s", compactMessagePrefix, FormatShortNumber(before.TokenEstimate), FormatShortNumber(after.TokenEstimate), summary)
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

func (c *Core) autoCompactSessionIfNeeded(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, report, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	if !report.Compact.Recommended || compactBackoffActive(messages) {
		return false, nil
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages)
}

func (c *Core) forceCompactSessionForRetry(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, _, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages)
}

func (c *Core) providerRequestNeedsCompact(ctx context.Context, turn turnExecution, request providers.Request) bool {
	sessionID := normalizeText(turn.SessionID)
	if sessionID == "" {
		return false
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}
	session = c.decorateSessionLLM(session)
	window := c.sessionContextWindowTokens(session)
	if window <= 0 {
		return false
	}
	threshold := compactThresholdForWindow(window)
	if threshold <= 0 {
		return false
	}
	return EstimateProviderRequestTokens(request) >= threshold
}

func (c *Core) sessionContextSnapshot(ctx context.Context, sessionID string) (Session, []Message, ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return Session{}, nil, ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	return session, messages, c.contextReportForSession(session, messages), nil
}

func (c *Core) compactSessionWithLoadedMessages(ctx context.Context, session Session, messages []Message) (bool, error) {
	_, effectiveMessages := latestCompactSummary(messages)
	if len(effectiveMessages) == 0 {
		return false, nil
	}
	_, err := c.CompactSession(ctx, session.ID)
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}
