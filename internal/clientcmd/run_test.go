package clientcmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func TestHelpFlagsPrintUsage(t *testing.T) {
	t.Setenv("MATRIXCLAW_SETUP_PATH", filepath.Join(t.TempDir(), "setup.json"))
	oldSetup, oldEnsure := openSetupUI, ensureDaemon
	t.Cleanup(func() { openSetupUI, ensureDaemon = oldSetup, oldEnsure })
	openSetupUI = func(context.Context, *appsetup.Service) (appsetup.ApplyResult, error) {
		t.Fatal("setup UI opened")
		return appsetup.ApplyResult{}, nil
	}
	ensureDaemon = func(context.Context, *appsetup.Service) (appsetup.DaemonSummary, error) {
		t.Fatal("daemon started")
		return appsetup.DaemonSummary{}, nil
	}
	for _, args := range [][]string{{"setup", "--help"}, {"tui", "-h"}, {"update", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", args); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Fatalf("%v: no usage: %q", args, stdout.String())
		}
	}
}

func TestUpdateRejectsUnknownSubcommand(t *testing.T) {
	t.Setenv("MATRIXCLAW_SETUP_PATH", filepath.Join(t.TempDir(), "setup.json"))
	oldCheck := checkLatestUpdate
	t.Cleanup(func() { checkLatestUpdate = oldCheck })
	checkLatestUpdate = nil
	var stdout, stderr bytes.Buffer
	if code := runUpdateCommand(&stdout, &stderr, "matrixclaw", []string{"bogus"}); code != 2 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr.String(), `unknown subcommand "bogus"`) || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
