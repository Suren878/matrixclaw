package externalagents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryProbeAsksForTheVersionOnce(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "fake-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho x >> '"+calls+"'\necho fake-agent 1.2.3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	probe := NewBinaryProbe("fake-agent", script)
	for range 3 {
		resolved, version, err := probe.Probe(context.Background())
		if err != nil || resolved != script || version != "fake-agent 1.2.3" {
			t.Fatalf("probe=(%q,%q,%v)", resolved, version, err)
		}
	}
	raw, _ := os.ReadFile(calls)
	if got := strings.Count(string(raw), "x"); got != 1 {
		t.Fatalf("--version ran %d times, want 1", got)
	}
}

func TestLookupBinaryRejectsAppBundles(t *testing.T) {
	if _, err := LookupBinary("codex", "/Applications/Codex.app/Contents/MacOS/codex"); err == nil || !strings.Contains(err.Error(), ".app bundle") {
		t.Fatalf("err=%v, want an app bundle error", err)
	}
}
