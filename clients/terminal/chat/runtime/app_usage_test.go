package runtime

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestHeaderShowsTheDaemonsContextMeasure(t *testing.T) {
	m, screen := renderApp(t, 100, 20, core.ClientSnapshot{SessionID: "session_1", Session: renderSession(), Context: renderContext()})
	if header := strings.SplitN(ansi.Strip(screen), "\n", 2)[0]; !strings.Contains(header, "Context: ~12k / 200k · gpt-test · openrouter") {
		t.Fatalf("header = %q", header)
	}

	deliver(t, m, core.EventContextUpdated, core.ContextUsage{SessionID: "session_1", TokenEstimate: 48_000, WindowTokens: 128_000})

	if header := strings.SplitN(ansi.Strip(m.viewContent()), "\n", 2)[0]; !strings.Contains(header, "Context: ~48k / 128k") {
		t.Fatalf("header after context.updated = %q", header)
	}
}

func TestExternalAgentHeaderNamesOnlyTheModel(t *testing.T) {
	session := renderSession()
	session.RuntimeID, session.ModelID = core.SessionRuntimeExternalAgent, "codex-mini"
	_, screen := renderApp(t, 100, 20, core.ClientSnapshot{SessionID: "session_1", Session: session, Context: renderContext()})
	header := strings.SplitN(ansi.Strip(screen), "\n", 2)[0]
	if !strings.HasSuffix(strings.TrimSpace(header), "· codex-mini") || strings.Contains(header, "openrouter") {
		t.Fatalf("header = %q", header)
	}
}
