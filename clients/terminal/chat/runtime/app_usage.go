package runtime

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// contextUsageText is the header's context line: the daemon's measure of the
// session's context, then the session's model and provider.
func (m *appModel) contextUsageText() string {
	snapshot := m.currentSnapshot()
	if snapshot.Session == nil || snapshot.Context == nil {
		return ""
	}
	parts := []string{formatHeaderContextUsage(*snapshot.Context)}
	provider, model := m.currentSessionLLM()
	for _, part := range []string{model, provider} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " · ")
}

func formatHeaderContextUsage(report core.ContextReport) string {
	if report.WindowTokens > 0 {
		return "Context: ~" + formatTokenCount(report.TokenEstimate) + " / " + formatTokenCount(report.WindowTokens)
	}
	return "Context: ~" + formatTokenCount(report.TokenEstimate)
}

func formatTokenCount(tokens int) string {
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
