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
	localTokens, visibleMarker := estimateVisibleContextTokens(snapshot.Messages)
	localTokens += m.assistantPromptTokens()
	tokens := localTokens
	if snapshot.Context != nil {
		if visibleMarker == headerContextMarkerNone || contextReportHasMarker(snapshot.Context, visibleMarker) {
			tokens = max(tokens, snapshot.Context.TokenEstimate)
		}
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

type headerContextMarker int

const (
	headerContextMarkerNone headerContextMarker = iota
	headerContextMarkerCompact
	headerContextMarkerClear
)

const (
	headerCompactSummaryPrefix = "🧠 Context compacted"
	headerContextClearedPrefix = "🧹 Context cleared"
)

func estimateVisibleContextTokens(messages []surfacemessage.Message) (int, headerContextMarker) {
	start := 0
	markerTokens := 0
	marker := headerContextMarkerNone
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != surfacemessage.System {
			continue
		}
		content := strings.TrimSpace(message.Content().Text)
		if summary, ok := headerCompactSummary(content); ok {
			start = i + 1
			markerTokens = estimateTokens(summary)
			marker = headerContextMarkerCompact
			break
		}
		if summary, ok := headerClearSummary(content); ok {
			start = i + 1
			markerTokens = estimateTokens(summary)
			marker = headerContextMarkerClear
			break
		}
	}
	return markerTokens + estimateMessagesTokens(messages[start:]), marker
}

func contextReportHasMarker(report *core.ContextReport, marker headerContextMarker) bool {
	if report == nil {
		return false
	}
	want := core.ContextBlockKind("")
	switch marker {
	case headerContextMarkerCompact:
		want = core.ContextBlockCompactSummary
	case headerContextMarkerClear:
		want = core.ContextBlockClearMarker
	default:
		return true
	}
	for _, block := range report.Blocks {
		if block.Kind == want {
			return true
		}
	}
	return false
}

func headerCompactSummary(content string) (string, bool) {
	if !strings.HasPrefix(content, headerCompactSummaryPrefix) {
		return "", false
	}
	summary := strings.TrimSpace(strings.TrimPrefix(content, headerCompactSummaryPrefix))
	if strings.HasPrefix(summary, ":") {
		if _, tail, ok := strings.Cut(summary, "\n\n"); ok {
			summary = strings.TrimSpace(tail)
		}
	}
	return summary, true
}

func headerClearSummary(content string) (string, bool) {
	if !strings.HasPrefix(content, headerContextClearedPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(content, headerContextClearedPrefix)), true
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
