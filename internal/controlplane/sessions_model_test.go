package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type sessionModelTestRuntime struct {
	sessions []core.Session
	updated  core.Session
}

func (r *sessionModelTestRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: "s1"}, nil
}

func (r *sessionModelTestRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return append([]core.Session(nil), r.sessions...), nil
}

func (r *sessionModelTestRuntime) CreateSession(context.Context, string, string, string) (core.Session, error) {
	return core.Session{}, nil
}

func (r *sessionModelTestRuntime) UseSession(context.Context, string, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: "s1"}, nil
}

func (r *sessionModelTestRuntime) RenameSession(context.Context, string, string) (core.Session, error) {
	return core.Session{}, nil
}

func (r *sessionModelTestRuntime) DeleteSession(context.Context, string) error {
	return nil
}

func (r *sessionModelTestRuntime) SessionModels(context.Context, string) (core.SessionModelsResponse, error) {
	return core.SessionModelsResponse{ModelID: "sonnet", Models: []string{"sonnet", "opus"}}, nil
}

func (r *sessionModelTestRuntime) UpdateSessionModel(_ context.Context, sessionID string, modelID string) (core.Session, error) {
	r.updated = core.Session{ID: sessionID, RuntimeID: core.SessionRuntimeExternalAgent, ModelID: modelID}
	return r.updated, nil
}

func TestSessionMenuIncludesModelAction(t *testing.T) {
	runtime := &sessionModelTestRuntime{sessions: []core.Session{{
		ID:        "s1",
		Title:     "Claude",
		RuntimeID: core.SessionRuntimeExternalAgent,
		ModelID:   "sonnet",
	}}}
	result, err := New(runtime, "").handleSessionMenu(context.Background(), "terminal", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("expected picker")
	}
	if !sessionModelPickerHasItem(result.Picker, "model") {
		t.Fatalf("model action missing: %#v", result.Picker.Items)
	}
}

func TestSessionListInfoNamesExternalAgent(t *testing.T) {
	info := sessionListInfo(core.Session{
		RuntimeID:         core.SessionRuntimeExternalAgent,
		ExternalAgentName: "Claude Code",
		ModelID:           "sonnet",
	})

	if info != "Claude Code · sonnet" {
		t.Fatalf("session list info = %q, want Claude Code · sonnet", info)
	}
}

func TestSessionModelPickerAndUpdate(t *testing.T) {
	runtime := &sessionModelTestRuntime{sessions: []core.Session{{ID: "s1", RuntimeID: core.SessionRuntimeExternalAgent, ModelID: "sonnet"}}}
	dispatcher := New(runtime, "")

	pickerResult, err := dispatcher.Handle(context.Background(), "terminal", "/session model s1")
	if err != nil {
		t.Fatal(err)
	}
	if pickerResult.Picker == nil || pickerResult.Picker.Kind != PickerSessionModels {
		t.Fatalf("model picker = %#v", pickerResult.Picker)
	}
	if pickerResult.Picker.Meta != "sonnet" {
		t.Fatalf("model picker meta = %q, want sonnet", pickerResult.Picker.Meta)
	}
	if !pickerResult.Picker.Popup || pickerResult.Picker.HasBack {
		t.Fatalf("model picker should be popup without back: %#v", pickerResult.Picker)
	}
	if !sessionModelPickerHasSelectedItem(pickerResult.Picker, "sonnet") || !sessionModelPickerHasItem(pickerResult.Picker, "opus") {
		t.Fatalf("model picker items = %#v", pickerResult.Picker.Items)
	}

	updateResult, err := dispatcher.Handle(context.Background(), "terminal", "/session set-model s1 opus")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.updated.ModelID != "opus" {
		t.Fatalf("updated model = %q", runtime.updated.ModelID)
	}
	if !updateResult.ReloadSnapshot {
		t.Fatal("expected reload snapshot")
	}
	if updateResult.Picker == nil || !sessionModelPickerHasItem(updateResult.Picker, "model") {
		t.Fatalf("expected refreshed session menu picker, got %#v", updateResult.Picker)
	}
}

func sessionModelPickerHasItem(picker *PickerData, id string) bool {
	for _, item := range picker.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func sessionModelPickerHasSelectedItem(picker *PickerData, id string) bool {
	for _, item := range picker.Items {
		if item.ID == id && item.Selected {
			return true
		}
	}
	return false
}
