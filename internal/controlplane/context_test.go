package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type contextTestRuntime struct {
	session        core.Session
	report         core.ContextReport
	systemMessages []string
}

func (r *contextTestRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: r.session.ID}, nil
}

func (r *contextTestRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return []core.Session{r.session}, nil
}

func (r *contextTestRuntime) CreateSession(context.Context, string, string, string) (core.Session, error) {
	return core.Session{}, nil
}

func (r *contextTestRuntime) UseSession(context.Context, string, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: r.session.ID}, nil
}

func (r *contextTestRuntime) RenameSession(context.Context, string, string) (core.Session, error) {
	return core.Session{}, nil
}

func (r *contextTestRuntime) DeleteSession(context.Context, string) error {
	return nil
}

func (r *contextTestRuntime) SessionContext(context.Context, string) (core.ContextReport, error) {
	return r.report, nil
}

func (r *contextTestRuntime) CompactSession(context.Context, string) (core.CompactSessionResult, error) {
	return core.CompactSessionResult{}, nil
}

func (r *contextTestRuntime) CreateSystemMessage(_ context.Context, _ string, content string) (core.Message, error) {
	r.systemMessages = append(r.systemMessages, content)
	return core.Message{Content: content}, nil
}

func TestContextRootPickerOffersClearCompactAndUsage(t *testing.T) {
	runtime := &contextTestRuntime{
		session: core.Session{ID: "s1"},
		report:  core.ContextReport{TokenEstimate: 42_000, WindowTokens: 128_000},
	}
	result, err := New(runtime, "").Handle(context.Background(), "terminal", "/context")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("Picker = nil")
	}
	if result.Picker.Title != "Context" {
		t.Fatalf("Picker.Title = %q, want Context", result.Picker.Title)
	}

	want := []struct {
		id      string
		title   string
		command string
	}{
		{id: "clear", title: "Clear context", command: "/context clear"},
		{id: "compact", title: "Compact", command: "/context compact"},
		{id: "info", title: "Usage", command: "/context info"},
	}
	if len(result.Picker.Items) != len(want) {
		t.Fatalf("picker items = %#v, want %d items", result.Picker.Items, len(want))
	}
	for i, expected := range want {
		item := result.Picker.Items[i]
		if item.ID != expected.id || item.Title != expected.title || item.Command != expected.command {
			t.Fatalf("item %d = %#v, want id=%q title=%q command=%q", i, item, expected.id, expected.title, expected.command)
		}
	}
}

func TestContextClearShowsConfirmation(t *testing.T) {
	result, err := New(&contextTestRuntime{session: core.Session{ID: "s1"}}, "").Handle(context.Background(), "terminal", "/context clear")
	if err != nil {
		t.Fatal(err)
	}
	if result.Confirm == nil {
		t.Fatal("Confirm = nil")
	}
	if result.Confirm.Message != "Clear context now?" {
		t.Fatalf("Confirm.Message = %q", result.Confirm.Message)
	}
	if result.Confirm.ConfirmCommand != "/context clear confirm" {
		t.Fatalf("ConfirmCommand = %q", result.Confirm.ConfirmCommand)
	}
	if result.Confirm.CancelCommand != "/context" {
		t.Fatalf("CancelCommand = %q", result.Confirm.CancelCommand)
	}
	if !result.Confirm.ConfirmDanger {
		t.Fatal("ConfirmDanger = false, want true")
	}
}

func TestContextClearConfirmAddsResetMarker(t *testing.T) {
	runtime := &contextTestRuntime{session: core.Session{ID: "s1"}}
	result, err := New(runtime, "").Handle(context.Background(), "terminal", "/context clear confirm")
	if err != nil {
		t.Fatal(err)
	}
	if !result.ReloadSnapshot {
		t.Fatal("ReloadSnapshot = false, want true")
	}
	if len(runtime.systemMessages) != 1 {
		t.Fatalf("systemMessages = %#v, want one", runtime.systemMessages)
	}
	if !strings.Contains(runtime.systemMessages[0], "Context cleared") {
		t.Fatalf("system message = %q, want context cleared marker", runtime.systemMessages[0])
	}
	if strings.Contains(runtime.systemMessages[0], "Context compacted") {
		t.Fatalf("system message = %q, must not use compact marker", runtime.systemMessages[0])
	}
}
