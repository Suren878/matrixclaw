package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInstallsManagedBrowser(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{
			name:    "npm install into managed runtime",
			command: "npm install --prefix /tmp/matrixclaw/runtime/browser/playwright-mcp @playwright/mcp@latest",
			blocked: true,
		},
		{
			name:    "install browser through playwright mcp",
			command: "playwright-mcp install-browser chrome-for-testing",
			blocked: true,
		},
		{
			name:    "install browser through npx playwright mcp",
			command: "npx @playwright/mcp@latest install-browser chrome-for-testing",
			blocked: true,
		},
		{
			name:    "managed browsers path through environment variable",
			command: "PLAYWRIGHT_BROWSERS_PATH=$MATRIXCLAW_RUNTIME_DIR/browser/ms-playwright playwright-mcp install-browser chrome-for-testing",
			blocked: true,
		},
		{
			name:    "quoted package through npm exec in a pipeline",
			command: "cd /tmp && npm exec -- '@playwright/mcp@0.0.40' install-browser chrome",
			blocked: true,
		},
		{
			name:    "binary by absolute path",
			command: "/opt/node/bin/playwright-mcp install-browser chromium",
			blocked: true,
		},
		{
			name:    "ordinary playwright browser install",
			command: "npx playwright install chromium",
			blocked: false,
		},
		{
			name:    "ordinary playwright mcp package install",
			command: "npm install @playwright/mcp",
			blocked: false,
		},
		{
			name:    "ordinary npm install",
			command: "npm install lodash",
			blocked: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := installsManagedBrowser(tc.command); got != tc.blocked {
				t.Fatalf("installsManagedBrowser(%q) = %t, want %t", tc.command, got, tc.blocked)
			}
		})
	}
}

func TestBashExecutorBlocksManagedBrowserInstall(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command: "playwright-mcp install-browser chrome-for-testing",
	})
	result, err := NewShellExecutors(nil)[0].Execute(context.Background(), Call{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError() {
		t.Fatalf("IsError = false, result = %#v", result)
	}
	if !strings.Contains(result.Content, "the managed browser is installed only through Modules") {
		t.Fatalf("content = %q, want managed Browser setup guidance", result.Content)
	}
}

func TestBashExecutorBlocksManagedBrowserInstallBeforeApproval(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command: "npm install --prefix /tmp/matrixclaw/runtime/browser/playwright-mcp @playwright/mcp@latest",
	})
	_, refused := NewRegistry(NewShellExecutors(nil)...).Preview(context.Background(), "bash", Call{Args: args})
	if refused == nil || !refused.IsError() {
		t.Fatalf("refused = %#v, want guard error without approval request", refused)
	}
	if !strings.Contains(refused.Content, "Modules -> Browser -> Engine") {
		t.Fatalf("content = %q, want Browser module setup guidance", refused.Content)
	}
}
