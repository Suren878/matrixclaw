package runtime

import (
	"fmt"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/agent/prompt"
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
	return agentcontext.EstimateTextTokens(prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)) + agentcontext.EstimateTextTokens(assistant.CustomInstructions)
}

type headerContextMarker int

const (
	headerContextMarkerNone headerContextMarker = iota
	headerContextMarkerCompact
	headerContextMarkerClear
)

func estimateVisibleContextTokens(messages []surfacemessage.Message) (int, headerContextMarker) {
	for i := len(messages) - 1; i >= 0; i-- {
		boundary := messages[i].Boundary
		if boundary == nil {
			continue
		}
		marker := headerContextMarkerCompact
		if boundary.Cleared {
			marker = headerContextMarkerClear
		}
		return agentcontext.EstimateTextTokens(boundary.Summary) + estimateMessagesTokens(messages[i+1:]), marker
	}
	return estimateMessagesTokens(messages), headerContextMarkerNone
}

func contextReportHasMarker(report *core.ContextReport, marker headerContextMarker) bool {
	if report == nil {
		return false
	}
	var want core.ContextBlockKind
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

func estimateMessagesTokens(messages []surfacemessage.Message) int {
	total := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			switch part := part.(type) {
			case surfacemessage.TextContent:
				total += agentcontext.EstimateTextTokens(part.Text)
			case surfacemessage.ImageURLContent:
				total += agentcontext.EstimatedImageTokens
			case surfacemessage.BinaryContent:
				total += agentcontext.EstimatedImageTokens
			case surfacemessage.ToolResult:
				total += agentcontext.EstimateTextTokens(part.Content)
			case surfacemessage.ToolCall:
				total += agentcontext.EstimateTextTokens(part.Input)
			}
		}
	}
	return total
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
