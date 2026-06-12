package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) contextReportForSession(session Session, messages []Message) ContextReport {
	report := c.contextReport(session.ID, messages)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	report.Compact = compactRecommendationForWindow(report.TokenEstimate, report.WindowTokens)
	return report
}

func (c *Core) contextReport(sessionID string, messages []Message) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := AssistantSystemPrompt(assistant)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	marker := latestContextMarker(messages)
	effectiveMessages := marker.effectiveMessages

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
	if marker.summary != "" {
		blocks = append(blocks, ContextBlock{
			ID:             marker.blockID,
			Kind:           marker.blockKind,
			Source:         marker.source,
			TokenEstimate:  EstimateTextTokens(marker.summary),
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
		total += EstimateTextTokens(tool.ID)
		total += EstimateTextTokens(tool.Description)
		total += EstimateTextTokens(string(tool.InputJSONSchema))
	}
	return total
}

func EstimateMessageTokens(messages []Message) int {
	total := 0
	for _, message := range messages {
		messageTotal := 0
		for _, part := range message.Parts {
			switch part.Kind {
			case MessagePartKindText:
				if part.Text != nil {
					messageTotal += EstimateTextTokens(part.Text.Text)
				}
			case MessagePartKindImage:
				if part.Image != nil {
					messageTotal += EstimatedImageTokens
				}
			case MessagePartKindToolCall:
				if part.ToolCall != nil {
					messageTotal += EstimateTextTokens(part.ToolCall.Name)
					messageTotal += EstimateTextTokens(part.ToolCall.Input)
				}
			case MessagePartKindToolResult:
				if part.ToolResult != nil {
					messageTotal += EstimateTextTokens(part.ToolResult.Name)
					messageTotal += EstimateTextTokens(providerVisibleToolResultContent(part.ToolResult.Name, part.ToolResult.Content))
				}
			}
		}
		if messageTotal == 0 {
			messageTotal = EstimateTextTokens(message.Content)
		}
		total += messageTotal
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

func compactRecommendationForWindow(tokens int, windowTokens int) ContextCompact {
	if windowTokens <= 0 {
		return compactRecommendation(tokens)
	}
	threshold := compactThresholdForWindow(windowTokens)
	if tokens < threshold {
		return ContextCompact{}
	}
	return ContextCompact{
		Recommended: true,
		Reason:      "estimated context is high for the selected model; compact session history before continuing",
	}
}

func compactThresholdForWindow(windowTokens int) int {
	if windowTokens <= 0 {
		return 0
	}
	threshold := windowTokens / 2
	const minimumThreshold = 80_000
	if threshold < minimumThreshold {
		threshold = minimumThreshold
	}
	return threshold
}

func EstimateProviderRequestTokens(request providers.Request) int {
	total := EstimateTextTokens(request.SystemPrompt) + EstimateTextTokens(request.CustomInstructions)
	for _, message := range request.Messages {
		total += EstimateTextTokens(message.Role)
		total += EstimateTextTokens(message.Content)
		if message.ReasoningContent != nil {
			total += EstimateTextTokens(*message.ReasoningContent)
		}
		total += len(message.Images) * EstimatedImageTokens
		total += EstimateTextTokens(message.ToolCallID)
		for _, call := range message.ToolCalls {
			total += EstimateTextTokens(call.Name)
			total += EstimateTextTokens(string(call.Arguments))
		}
	}
	for _, tool := range request.Tools {
		total += EstimateTextTokens(tool.Name)
		total += EstimateTextTokens(tool.Description)
		total += EstimateTextTokens(string(tool.InputSchema))
	}
	return total
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
