package runtime

import (
	"strings"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

// contextUsageText is the header's context line: the session goal, the
// daemon's measure of the session's context, then the session's model and
// provider.
func (m *appModel) contextUsageText() string {
	snapshot := m.state()
	if snapshot.Session() == nil || snapshot.Context() == nil {
		return ""
	}
	var parts []string
	if goal := controlplane.GoalLine(snapshot.Goal()); goal != "" {
		parts = append(parts, goal)
	}
	parts = append(parts, formatHeaderContextUsage(*snapshot.Context()))
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
		return "Context: ~" + surfacecommon.FormatTokens(report.TokenEstimate) + " / " + surfacecommon.FormatTokens(report.WindowTokens)
	}
	return "Context: ~" + surfacecommon.FormatTokens(report.TokenEstimate)
}
