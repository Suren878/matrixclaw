package telegram

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnreadableReferenceFileIsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refs.json")
	if err := os.WriteFile(path, []byte(`{"old":"format"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newRefStore(path, 10)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unreadable file kept: %v", err)
	}
	store.put("k", "/sessions")
	if got, ok := newRefStore(path, 10).get("k"); !ok || got != "/sessions" {
		t.Fatalf("saved reference = %q, %v", got, ok)
	}
}
