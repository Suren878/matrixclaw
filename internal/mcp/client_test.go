package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestBrowserToolsAskUnlessTheyOnlyReadThePage(t *testing.T) {
	server := ServerConfig{ID: "browser", ToolPrefix: "browser"}
	for name, wantAsk := range map[string]bool{
		"browser_snapshot":         false,
		"browser_console_messages": false,
		"browser_navigate":         true,
		"browser_evaluate":         true,
		"browser_file_upload":      true,
		"browser_click":            true,
	} {
		executor := newRemoteToolExecutor(server, &Session{session: &sdk.ClientSession{}}, &sdk.Tool{Name: name})
		if spec := executor.Spec(); spec.Mutates() != wantAsk || spec.Asks != wantAsk {
			t.Errorf("%s: mutates=%v asks=%v, want %v", name, spec.Mutates(), spec.Asks, wantAsk)
		}
	}
	readOnly := newRemoteToolExecutor(ServerConfig{ID: "docs", ReadOnly: true}, &Session{}, &sdk.Tool{Name: "search"})
	if spec := readOnly.Spec(); spec.Asks || spec.Mutates() {
		t.Errorf("read-only server tool: %+v", spec)
	}
	preview, err := newRemoteToolExecutor(server, &Session{}, &sdk.Tool{Name: "browser_click"}).(tools.Previewer).Preview(context.Background(), tools.Call{Args: []byte(`{"ref":"e1"}`)})
	if err != nil || preview.Description != "Call remote MCP tool browser_click on browser" {
		t.Errorf("preview = %+v, %v", preview, err)
	}
}
