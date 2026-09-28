package mcp

import (
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestRemoteToolSubjectNamesServerAndTool(t *testing.T) {
	executor := newRemoteToolExecutor(ServerConfig{ID: "github", ToolPrefix: "gh"}, nil, &sdk.Tool{Name: "create_issue"})
	registry := tools.NewRegistry(executor)

	got := registry.Subject(executor.Spec().ID, tools.Call{})

	if want := (permission.Subject{Kind: permission.KindName, Value: "github__create_issue"}); got != want {
		t.Fatalf("subject = %+v, want %+v", got, want)
	}
}
