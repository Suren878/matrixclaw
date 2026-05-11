package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRebuildChatPreservesSelectedMessageWhenNotFollowing(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first", "second"))
	model.rebuildChat()

	if model.chat == nil {
		t.Fatal("expected chat model")
	}

	model.chat.SelectFirst()
	model.chat.ScrollToSelected()

	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first", "second", "third"))
	model.rebuildChat()

	if got := model.chat.SelectedMessageID(); got != "msg-1" {
		t.Fatalf("expected selected message to stay on first item, got %q", got)
	}
}

func TestRebuildChatKeepsTailWhenFollowing(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first", "second"))
	model.rebuildChat()

	if model.chat == nil {
		t.Fatal("expected chat model")
	}

	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first", "second", "third"))
	model.rebuildChat()

	if got := model.chat.SelectedMessageID(); got != "msg-3" {
		t.Fatalf("expected selected message to stay on newest item, got %q", got)
	}
}

func TestWorkingStatusViewRendersBusyRunAboveEditor(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, &Runtime{config: Config{
		Provider: "OpenAI",
		Model:    "gpt-5.4-mini",
	}})
	model.width = 100
	model.height = 30
	model.now = now.Add(75 * time.Second)
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Run: &core.Run{
			ID:        "run-1",
			SessionID: "session-1",
			Status:    core.RunStatusRunning,
			StartedAt: now,
			UpdatedAt: now,
		},
	})
	model.setBusy(true)

	view := model.workingStatusView()
	if !strings.Contains(view, "[gpt-5.4-mini]") {
		t.Fatalf("workingStatusView() = %q, want model label", view)
	}
	if !strings.Contains(view, "Waiting for model (1m 15s") {
		t.Fatalf("workingStatusView() = %q, want model wait text", view)
	}
	if !strings.Contains(view, "esc to cancel") {
		t.Fatalf("workingStatusView() = %q, want cancel hint", view)
	}
}

func TestWorkingStatusViewShowsToolPhase(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, &Runtime{config: Config{
		Provider: "OpenAI",
		Model:    "gpt-5.4-mini",
	}})
	model.width = 100
	model.now = now.Add(12 * time.Second)
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Run: &core.Run{
			ID:        "run-1",
			SessionID: "session-1",
			Status:    core.RunStatusRunning,
			StartedAt: now,
			UpdatedAt: now,
		},
		ToolUpdates: []core.ToolUpdate{{
			ToolCallID: "tool-1",
			ToolName:   "bash",
			State:      core.ToolLifecycleRequested,
		}},
	})
	model.setBusy(true)

	view := model.workingStatusView()
	if !strings.Contains(view, "Running Run (12s") {
		t.Fatalf("workingStatusView() = %q, want running tool phase", view)
	}
}

func TestInputSectionViewAddsBlankLineAboveEditor(t *testing.T) {
	model := newApp(nil, nil)
	model.width = 80

	view := model.inputSectionView()

	if !strings.HasPrefix(view, "\n") {
		t.Fatalf("inputSectionView() = %q, want leading blank line", view)
	}
}

func TestLayoutBodyAndEditorBoundsShareInputBoundary(t *testing.T) {
	model := newApp(nil, nil)
	model.width = 80
	model.height = 24

	layout := model.layout()

	if layout.bodyBottom != layout.editorTop {
		t.Fatalf("bodyBottom = %d, editorTop = %d, want shared boundary", layout.bodyBottom, layout.editorTop)
	}
	bodyTop, bodyBottom := model.bodyBounds()
	if bodyTop != layout.bodyTop || bodyBottom != layout.bodyBottom {
		t.Fatalf("bodyBounds() = (%d,%d), layout = (%d,%d)", bodyTop, bodyBottom, layout.bodyTop, layout.bodyBottom)
	}
	editorTop, editorBottom := model.editorBounds()
	if editorTop != layout.editorTop || editorBottom != layout.editorBottom {
		t.Fatalf("editorBounds() = (%d,%d), layout = (%d,%d)", editorTop, editorBottom, layout.editorTop, layout.editorBottom)
	}
}

func TestLayoutClampsTinyScreens(t *testing.T) {
	model := newApp(nil, nil)
	model.width = 20
	model.height = 3

	layout := model.layout()

	if layout.headerHeight != 0 || layout.footerHeight != 0 {
		t.Fatalf("tiny layout chrome = header %d footer %d, want 0/0", layout.headerHeight, layout.footerHeight)
	}
	if layout.bodyTop > layout.bodyBottom {
		t.Fatalf("tiny layout body inverted: top=%d bottom=%d", layout.bodyTop, layout.bodyBottom)
	}
	if layout.editorTop > layout.editorBottom {
		t.Fatalf("tiny layout editor inverted: top=%d bottom=%d", layout.editorTop, layout.editorBottom)
	}
}

func TestHeaderViewUsesCompactShellBelowBreakpoint(t *testing.T) {
	model := newApp(nil, nil)
	model.width = 100
	model.height = 29
	model.session = "session-1"
	model.workingDir = "/workspace/matrixclaw"

	view := model.headerView()
	if strings.Contains(view, "ctrl+d") {
		t.Fatalf("compact header contains removed details shortcut: %q", view)
	}
	if !strings.Contains(view, "matrixclaw v0.1.0") {
		t.Fatalf("compact header missing product title: %q", view)
	}
	if strings.Contains(view, "/workspace/matrixclaw") {
		t.Fatalf("compact header unexpectedly contains working directory: %q", view)
	}
}

func TestCurrentSessionTitleUsesMatrixclawFallback(t *testing.T) {
	model := newApp(nil, nil)
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{SessionID: "session-1"})

	if got := model.currentSessionTitle(); got != "matrixclaw" {
		t.Fatalf("currentSessionTitle() = %q, want matrixclaw", got)
	}
}

func TestHeaderViewUsesFullShellAboveBreakpoint(t *testing.T) {
	model := newApp(nil, nil)
	model.width = 160
	model.height = 40
	model.session = "session-1"
	model.workingDir = "/workspace/matrixclaw"

	view := model.headerView()
	if strings.Contains(view, "ctrl+d") {
		t.Fatalf("full header should not show compact details shortcut: %q", view)
	}
	if !strings.Contains(view, "matrixclaw v0.1.0") {
		t.Fatalf("full header missing product title: %q", view)
	}
	if strings.Contains(view, "/workspace/matrixclaw") {
		t.Fatalf("full header unexpectedly contains working directory: %q", view)
	}
}

func TestHeaderViewDoesNotExposeFileHistoryWorkingDir(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.width = 100
	model.height = 29
	model.session = "session-1"
	model.workingDir = ""
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Files: []core.FileSnapshot{
			{ID: "file-1", SessionID: "session-1", Path: "/workspace/matrixclaw/internal/a.txt", Content: "one", Version: 0, CreatedAt: now, UpdatedAt: now},
			{ID: "file-2", SessionID: "session-1", Path: "/workspace/matrixclaw/cmd/main.go", Content: "two", Version: 0, CreatedAt: now, UpdatedAt: now},
		},
	})

	view := model.headerView()
	if !strings.Contains(view, "matrixclaw v0.1.0") {
		t.Fatalf("header missing product title: %q", view)
	}
	if strings.Contains(view, "/workspace/matrixclaw") {
		t.Fatalf("header unexpectedly contains working directory from file history: %q", view)
	}
}

func TestHeaderViewShowsModelProviderAndApproxTokens(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.width = 100
	model.height = 29
	model.session = "session-1"
	snapshot := snapshotWithTexts(now, "hello world")
	snapshot.Session = &core.Session{ID: "session-1", ProviderID: "gemini", ModelID: "gemma-3"}
	model.read = viewmodel.NewReadModel(snapshot)

	view := model.headerView()
	for _, want := range []string{"matrixclaw v0.1.0", "gemma-3", "gemini", "Context:", "tokens"} {
		if !strings.Contains(view, want) {
			t.Fatalf("header = %q, want %q", view, want)
		}
	}
}

func TestHeaderViewIncludesAssistantPromptTokens(t *testing.T) {
	model := newApp(nil, &Runtime{config: Config{
		Assistant: core.AssistantProfile{
			Name:               "Clawdia",
			SystemPrompt:       "Base system prompt with project context.",
			CustomInstructions: "Prefer short answers.",
		},
	}})
	model.width = 100
	model.height = 29
	model.session = "session-1"
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Session:   &core.Session{ID: "session-1"},
	})

	wantTokens := estimateTokens(core.AssistantSystemPrompt(model.rt.config.Assistant)) + estimateTokens(model.rt.config.Assistant.CustomInstructions)
	view := model.headerView()
	want := "Context: ~" + formatTokenCount(wantTokens) + " tokens"
	if !strings.Contains(view, want) {
		t.Fatalf("header = %q, want assistant prompt tokens %q", view, want)
	}
}
