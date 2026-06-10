package daemoncmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestRuntimeStatusPromptReportsBrowserInstallAndAvailability(t *testing.T) {
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := setup.NewService(store)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon:  setup.DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
		Modules: setup.ModulesConfig{
			Browser: setup.BrowserConfig{
				Enabled:    true,
				ProviderID: setup.BrowserProviderPlaywright,
				ProviderConfig: setup.BrowserProviderConfig{
					RuntimeMode: "per_task",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	prompt := (&setupRuntimeStatusContext{
		setup:   service,
		runtime: localruntime.New(""),
	}).RuntimeStatusPromptContext(context.Background(), core.RuntimeStatusContextRequest{
		ToolIDs: []string{"web_search", "web_fetch"},
	})

	for _, want := range []string{
		"Current runtime status (fresh for this request):",
		"browser: enabled=true; provider=Local Playwright; mode=per_task; runtime_installed=false; browser_installed=false; browser_tools=unavailable",
		"web_search: provider=DuckDuckGo; tools=web_search,web_fetch",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("runtime status prompt missing %q:\n%s", want, prompt)
		}
	}
}
