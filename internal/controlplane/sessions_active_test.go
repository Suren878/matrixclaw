package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionsPickerLabelsCurrentSessionActive(t *testing.T) {
	runtime := &sessionModelTestRuntime{sessions: []core.Session{
		{ID: "s1", Title: "Main"},
		{ID: "s2", Title: "Other"},
	}}
	result, err := New(runtime, "").Handle(context.Background(), "terminal", "/sessions")
	if err != nil {
		t.Fatal(err)
	}

	item := requirePickerItem(t, result.Picker, "s1")
	if !item.Selected {
		t.Fatal("current session Selected = false, want true")
	}
	if !strings.Contains(item.Info, "Active") {
		t.Fatalf("current session Info = %q, want Active label", item.Info)
	}
}
