package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const compactMessagePrefix = "🧠 Context compacted"
const maxCompactPromptRunes = 80_000

type CompactSessionResult struct {
	Message Message       `json:"message"`
	Context ContextReport `json:"context"`
}

type ContextBlockKind string

const (
	ContextBlockSystemPrompt       ContextBlockKind = "system_prompt"
	ContextBlockCustomInstructions ContextBlockKind = "custom_instructions"
	ContextBlockCompactSummary     ContextBlockKind = "compact_summary"
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

func (c *Core) SessionContext(ctx context.Context, sessionID string) (ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return ContextReport{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return ContextReport{}, err
	}
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return ContextReport{}, err
	}
	return c.contextReport(sessionID, messages), nil
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
	before := c.contextReport(session.ID, messages)
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
	after := c.contextReport(session.ID, append(append([]Message(nil), messages...), preview))
	content := fmt.Sprintf("%s: ~%s -> ~%s tokens\n\n%s", compactMessagePrefix, FormatShortNumber(before.TokenEstimate), FormatShortNumber(after.TokenEstimate), summary)
	message, err := c.CreateSystemMessage(ctx, session.ID, content)
	if err != nil {
		return CompactSessionResult{}, err
	}

	nextMessages, err := c.store.ListMessages(ctx, session.ID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReport(session.ID, nextMessages)}, nil
}

func (c *Core) contextReport(sessionID string, messages []Message) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := AssistantSystemPrompt(assistant)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	compactSummary, effectiveMessages := latestCompactSummary(messages)

	blocks := make([]ContextBlock, 0, 4)
	if systemPrompt != "" {
		blocks = append(blocks, ContextBlock{
			ID:             "system",
			Kind:           ContextBlockSystemPrompt,
			Source:         "assistant_profile",
			TokenEstimate:  EstimateTextTokens(systemPrompt),
			Included:       true,
			CacheStability: "stable",
		})
	}
	if customInstructions != "" {
		blocks = append(blocks, ContextBlock{
			ID:             "custom_instructions",
			Kind:           ContextBlockCustomInstructions,
			Source:         "assistant_profile",
			TokenEstimate:  EstimateTextTokens(customInstructions),
			Included:       true,
			CacheStability: "stable",
		})
	}
	if compactSummary != "" {
		blocks = append(blocks, ContextBlock{
			ID:             "compact_summary",
			Kind:           ContextBlockCompactSummary,
			Source:         "session_compact",
			TokenEstimate:  EstimateTextTokens(compactSummary),
			Included:       true,
			CacheStability: "stable",
		})
	}
	if len(effectiveMessages) > 0 {
		blocks = append(blocks, ContextBlock{
			ID:             "messages",
			Kind:           ContextBlockMessages,
			Source:         "session_history",
			TokenEstimate:  EstimateMessageTokens(effectiveMessages),
			Included:       true,
			CacheStability: "dynamic",
		})
	}
	if estimate := c.estimateToolSchemaTokens(); estimate > 0 {
		blocks = append(blocks, ContextBlock{
			ID:             "tools",
			Kind:           ContextBlockToolSchemas,
			Source:         "tool_registry",
			TokenEstimate:  estimate,
			Included:       true,
			CacheStability: "stable",
		})
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
		MessageCount:      len(effectiveMessages),
		Blocks:            blocks,
		LastProviderUsage: latestProviderUsage(effectiveMessages),
		Compact:           compactRecommendation(total),
	}
}

func (c *Core) generateCompactSummary(ctx context.Context, session Session, messages []Message) (string, error) {
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return "", err
	}

	response, err := runtime.Generate(ctx, providers.Request{
		SessionID: session.ID,
		SystemPrompt: strings.TrimSpace(`You compact matrixclaw chat histories.
Return concise durable context for future assistant turns.
Keep only reusable facts: current goal, decisions, constraints, files/modules changed, pending tasks, and known failures.
Do not add filler.`),
		Messages: []providers.Message{{
			Role:    "user",
			Content: "Compact this session history:\n\n" + compactHistoryPrompt(messages),
		}},
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", errors.New("compact summary is empty")
	}
	return text, nil
}

func compactHistoryPrompt(messages []Message) string {
	var builder strings.Builder
	for _, message := range messages {
		if message.Role == MessageRoleSystem {
			continue
		}
		role := strings.TrimSpace(string(message.Role))
		text := strings.TrimSpace(message.Content)
		if text == "" {
			text = compactMessagePartsText(message.Parts)
		}
		if role == "" || text == "" {
			continue
		}
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(text)
		builder.WriteString("\n\n")
	}
	return trimRunesFromStart(builder.String(), maxCompactPromptRunes)
}

func compactMessagePartsText(parts []MessagePart) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != nil:
			values = append(values, part.Text.Text)
		case part.Image != nil:
			values = append(values, "image: "+imagePartLabel(*part.Image))
		case part.ToolCall != nil:
			values = append(values, "tool call: "+part.ToolCall.Name+" "+part.ToolCall.Input)
		case part.ToolResult != nil:
			values = append(values, "tool result: "+part.ToolResult.Name+" "+part.ToolResult.Content)
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n"))
}

func trimRunesFromStart(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return "[older history truncated]\n" + string(runes[len(runes)-maxRunes:])
}

func latestCompactSummary(messages []Message) (string, []Message) {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != MessageRoleSystem {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if !strings.HasPrefix(content, compactMessagePrefix) {
			continue
		}
		summary := strings.TrimSpace(strings.TrimPrefix(content, compactMessagePrefix))
		return summary, messages[i+1:]
	}
	return "", messages
}

func (c *Core) estimateToolSchemaTokens() int {
	if c.tools == nil {
		return 0
	}
	total := 0
	for _, tool := range c.tools.List() {
		total += EstimateTextTokens(tool.ID)
		total += EstimateTextTokens(tool.Description)
		total += EstimateTextTokens(string(tool.InputJSONSchema))
	}
	return total
}

func EstimateMessageTokens(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += EstimateTextTokens(message.Content)
		for _, part := range message.Parts {
			switch part.Kind {
			case MessagePartKindText:
				if part.Text != nil {
					total += EstimateTextTokens(part.Text.Text)
				}
			case MessagePartKindImage:
				if part.Image != nil {
					total += EstimateTextTokens(imagePartLabel(*part.Image))
				}
			case MessagePartKindToolCall:
				if part.ToolCall != nil {
					total += EstimateTextTokens(part.ToolCall.Name)
					total += EstimateTextTokens(part.ToolCall.Input)
				}
			case MessagePartKindToolResult:
				if part.ToolResult != nil {
					total += EstimateTextTokens(part.ToolResult.Name)
					total += EstimateTextTokens(part.ToolResult.Content)
				}
			}
		}
	}
	return total
}

func EstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return max(1, (utf8.RuneCountInString(text)+3)/4)
}

func compactRecommendation(tokens int) ContextCompact {
	const defaultCompactThreshold = 80_000
	if tokens < defaultCompactThreshold {
		return ContextCompact{}
	}
	return ContextCompact{
		Recommended: true,
		Reason:      "estimated context is high; compact session history before continuing",
	}
}

func FormatShortNumber(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 10_000:
		return fmt.Sprintf("%.0fk", float64(value)/1_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func latestProviderUsage(messages []Message) *ProviderUsage {
	for i := len(messages) - 1; i >= 0; i-- {
		for j := len(messages[i].Parts) - 1; j >= 0; j-- {
			part := messages[i].Parts[j]
			if part.Kind != MessagePartKindFinish || part.Finish == nil || len(part.Finish.Details) == 0 {
				continue
			}
			var payload struct {
				Usage ProviderUsage `json:"usage"`
			}
			if err := json.Unmarshal(part.Finish.Details, &payload); err == nil && !providerUsageEmpty(payload.Usage) {
				usage := payload.Usage
				return &usage
			}
		}
	}
	return nil
}

func providerUsageEmpty(usage ProviderUsage) bool {
	return usage.InputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.TotalTokens == 0 &&
		usage.CachedTokens == 0 &&
		usage.ReasoningTokens == 0 &&
		len(usage.ProviderRaw) == 0
}
