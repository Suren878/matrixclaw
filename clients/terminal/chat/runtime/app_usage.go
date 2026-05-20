package runtime

import (
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

	parts := []string{formatHeaderContextUsage(tokens, snapshot.Context)}
	if model != "" {
		parts = append(parts, model)
	}
	if provider != "" {
		parts = append(parts, provider)
	}
	return strings.Join(parts, " · ")
}

func formatHeaderContextUsage(tokens int, report *core.ContextReport) string {
	if report != nil {
		if report.TokenEstimate > tokens {
			tokens = report.TokenEstimate
		}
		if report.WindowTokens > 0 {
			return "Context: ~" + formatTokenCount(tokens) + " / " + formatTokenCount(report.WindowTokens)
		}
	}
	return "Context: ~" + formatTokenCount(tokens)
}

func sessionIsExternalAgent(session *core.Session) bool {
	if session == nil {
		return false
	}
	return core.NormalizeSessionRuntime(session.RuntimeID) == core.SessionRuntimeExternalAgent ||
		core.NormalizeSessionKind(session.Kind) == core.SessionKindExternalAgent
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
			case surfacemessage.ImageURLContent:
				total += core.EstimatedImageTokens
			case surfacemessage.BinaryContent:
				total += core.EstimatedImageTokens
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
