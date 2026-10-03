package mcp

import (
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
		spec := newRemoteToolExecutor(server, &clientSession{}, &sdk.Tool{Name: name}).Spec()
		if spec.RequiresApproval() != wantAsk || spec.Mutates() != wantAsk {
			t.Errorf("%s: asks=%v mutates=%v, want %v", name, spec.RequiresApproval(), spec.Mutates(), wantAsk)
		}
	}
}
