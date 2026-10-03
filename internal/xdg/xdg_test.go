package xdg

import (
	"path/filepath"
	"testing"
)

func TestStateHomePrefersXDGThenHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got := StateHome(); got != "/xdg/state" {
		t.Fatalf("StateHome = %q, want /xdg/state", got)
	}
	t.Setenv("XDG_STATE_HOME", " ")
	t.Setenv("HOME", "/home/someone")
	if got, want := StateHome(), filepath.Join("/home/someone", ".local", "state"); got != want {
		t.Fatalf("StateHome = %q, want %q", got, want)
	}
}
