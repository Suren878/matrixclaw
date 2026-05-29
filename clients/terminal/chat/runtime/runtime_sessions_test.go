package runtime

import "testing"

func TestInitialLocalSessionTitleIsMain(t *testing.T) {
	if got := defaultInitialSessionTitle(); got != "Main" {
		t.Fatalf("defaultInitialSessionTitle() = %q, want %q", got, "Main")
	}
}

func TestManualNewSessionTitleIsTemporary(t *testing.T) {
	if got := defaultNewSessionTitle(); got != "New chat" {
		t.Fatalf("defaultNewSessionTitle() = %q, want %q", got, "New chat")
	}
}
