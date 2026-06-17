package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBlockedManagedBrowserInstallCommand(t *testing.T) {
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
			if got := blockedManagedBrowserInstallCommand(tc.command); got != tc.blocked {
				t.Fatalf("blockedManagedBrowserInstallCommand(%q) = %t, want %t", tc.command, got, tc.blocked)
			}
		})
	}
}

func TestBashExecutorBlocksManagedBrowserInstallEvenWhenApproved(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command: "playwright-mcp install-browser chrome-for-testing",
	})
	result, err := NewBashExecutor().Execute(context.Background(), Call{
		Args:     args,
		Approved: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, result = %#v", result)
	}
	if !strings.Contains(result.Content, "Managed Browser setup is only available through Modules") {
		t.Fatalf("content = %q, want managed Browser setup guidance", result.Content)
	}
}

func TestBashExecutorBlocksManagedBrowserInstallBeforeApproval(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command: "npm install --prefix /tmp/matrixclaw/runtime/browser/playwright-mcp @playwright/mcp@latest",
	})
	result, err := NewBashExecutor().Execute(context.Background(), Call{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Approval != nil {
		t.Fatalf("result = %#v, want guard error without approval request", result)
	}
	if !strings.Contains(result.Content, "Modules -> Browser -> Install/Repair") {
		t.Fatalf("content = %q, want Browser module setup guidance", result.Content)
	}
}
