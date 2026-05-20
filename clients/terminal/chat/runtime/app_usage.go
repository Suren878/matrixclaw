package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) contextUsageText() string {
	if m.read == nil {
		return ""
	}
	snapshot := m.currentSnapshot()
	if snapshot.Session == nil && len(snapshot.Messages) == 0 {
		return ""
	}
	model := strings.TrimSpace(m.currentModelLabel())
	provider, _ := m.currentSessionLLM()
	if provider = strings.TrimSpace(provider); provider == "" && !sessionIsExternalAgent(snapshot.Session) {
		provider = strings.TrimSpace(m.providerName)
	}
	tokens := estimateMessagesTokens(snapshot.Messages) + m.assistantPromptTokens()
	if snapshot.Context != nil && snapshot.Context.TokenEstimate > tokens {
		tokens = snapshot.Context.TokenEstimate
	}

	parts := []string{"Context: ~" + formatTokenCount(tokens) + " tokens"}
	if usage, ok := latestHeaderProviderUsage(snapshot.Context, snapshot.Messages); ok {
		parts = append(parts, formatHeaderProviderUsage(usage))
	}
	if model != "" {
		parts = append(parts, model)
	}
	if provider != "" {
		parts = append(parts, provider)
	}
	return strings.Join(parts, " · ")
}

func sessionIsExternalAgent(session *core.Session) bool {
	if session == nil {
		return false
	}
	return core.NormalizeSessionRuntime(session.RuntimeID) == core.SessionRuntimeExternalAgent ||
		core.NormalizeSessionKind(session.Kind) == core.SessionKindExternalAgent
}

func latestHeaderProviderUsage(report *core.ContextReport, messages []surfacemessage.Message) (core.ProviderUsage, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		for j := len(messages[i].Parts) - 1; j >= 0; j-- {
			finish, ok := messages[i].Parts[j].(surfacemessage.Finish)
			if !ok || strings.TrimSpace(finish.Details) == "" {
				continue
			}
			usage, ok := providerUsageFromFinishDetails(finish.Details)
			if ok {
				return usage, true
			}
		}
	}
	if report != nil && report.LastProviderUsage != nil && !providerUsageEmptyForHeader(*report.LastProviderUsage) {
		return *report.LastProviderUsage, true
	}
	return core.ProviderUsage{}, false
}

func providerUsageFromFinishDetails(details string) (core.ProviderUsage, bool) {
	var payload struct {
		Usage core.ProviderUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(details), &payload); err != nil || providerUsageEmptyForHeader(payload.Usage) {
		return core.ProviderUsage{}, false
	}
	return payload.Usage, true
}

func formatHeaderProviderUsage(usage core.ProviderUsage) string {
	input := usage.InputTokens
	output := usage.OutputTokens
	total := usage.TotalTokens
	if total == 0 {
		total = input + output
	}
	segments := make([]string, 0, 4)
	if input > 0 {
		segments = append(segments, "in "+formatTokenCount64(input))
	}
	if output > 0 {
		segments = append(segments, "out "+formatTokenCount64(output))
	}
	if usage.ReasoningTokens > 0 {
		segments = append(segments, "reasoning "+formatTokenCount64(usage.ReasoningTokens))
	}
	if usage.CachedTokens > 0 {
		segments = append(segments, "cached "+formatTokenCount64(usage.CachedTokens))
	}
	if len(segments) == 0 && total > 0 {
		segments = append(segments, formatTokenCount64(total))
	}
	if len(segments) == 0 {
		return ""
	}
	return "Last: " + strings.Join(segments, " / ")
}

func providerUsageEmptyForHeader(usage core.ProviderUsage) bool {
	return usage.InputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.TotalTokens == 0 &&
		usage.CachedTokens == 0 &&
		usage.ReasoningTokens == 0
}

func (m *appModel) assistantPromptTokens() int {
	if m == nil || m.rt == nil {
		return 0
	}
	assistant := m.rt.config.Assistant
	return core.EstimateTextTokens(core.AssistantSystemPrompt(assistant)) + core.EstimateTextTokens(assistant.CustomInstructions)
}

func estimateMessagesTokens(messages []surfacemessage.Message) int {
	total := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			switch part := part.(type) {
			case surfacemessage.TextContent:
				total += estimateTokens(part.Text)
			case surfacemessage.ToolResult:
				total += estimateTokens(part.Content)
			case surfacemessage.ToolCall:
				total += estimateTokens(part.Input)
			}
		}
	}
	return total
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	// Fast provider-neutral estimate. Exact tokenization is provider/model-specific.
	return max(1, (utf8.RuneCountInString(text)+3)/4)
}

func formatTokenCount(tokens int) string {
	return formatTokenCount64(int64(tokens))
}

func formatTokenCount64(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 10_000:
		return fmt.Sprintf("%.0fk", float64(tokens)/1_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}
