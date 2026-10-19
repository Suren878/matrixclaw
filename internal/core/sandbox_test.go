package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/sandbox"
)

// TestMain lets the test binary act as the daemon's Landlock helper.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == sandbox.HelperCommand {
		os.Exit(sandbox.RunHelper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func TestFullAutoCommandsWriteOnlyTheWorkspace(t *testing.T) {
	if sandbox.Detect().Mechanism == "" {
		t.Skip("commands cannot be confined here: " + sandbox.Detect().Detail)
	}
	app, db, _, _ := newTaskCore(t)
	app.WithSandbox(func() sandbox.Settings { return sandbox.Settings{} })
	session := permissionSession(t, db, "session_sandboxed", t.TempDir(), core.PermissionModeFullAuto, "")
	// The temporary directories are writable, so the refused place is elsewhere.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outside, err := os.MkdirTemp(wd, "sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })

	inside := foreground("touch inside")
	inside.WorkingDir = session.WorkingDir
	result, err := app.RunCommand(context.Background(), taskCall(session), inside)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("write inside = %+v, %v", result, err)
	}
	result, err = app.RunCommand(context.Background(), taskCall(session), foreground("touch "+filepath.Join(outside, "x")))
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 || result.SandboxHint == "" {
		t.Fatalf("write outside = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(outside, "x")); !os.IsNotExist(err) {
		t.Fatalf("the refused file exists: %v", err)
	}
}
