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
		if executor.Spec().Mutates() != wantAsk {
			t.Errorf("%s: mutates=%v, want %v", name, executor.Spec().Mutates(), wantAsk)
		}
		if !wantAsk {
			continue
		}
		if result, err := executor.Execute(context.Background(), tools.Call{}); err != nil || result.Approval == nil {
			t.Errorf("%s: ran without approval (err=%v)", name, err)
		}
	}
}
