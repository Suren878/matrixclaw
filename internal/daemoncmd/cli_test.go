package daemoncmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunCLIHelpDoesNotStartDaemon(t *testing.T) {
	var stdout bytes.Buffer
	called := false

	code := RunCLI(context.Background(), &stdout, "matrixclawd", []string{"--help"}, func(context.Context) error {
		called = true
		return nil
	})

	if code != 0 {
		t.Fatalf("RunCLI() code = %d, want 0", code)
	}
	if called {
		t.Fatal("RunCLI() started daemon for --help")
	}
	if !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stdout.String(), "matrixclawd") {
		t.Fatalf("help output = %q, want usage for matrixclawd", stdout.String())
	}
}
