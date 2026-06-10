package mcp

import (
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestBrowserToolsDoNotRequireApproval(t *testing.T) {
	navigate := newRemoteToolExecutor(ServerConfig{ID: "browser", ToolPrefix: "browser"}, nil, &sdk.Tool{Name: "browser_navigate"}).Spec()
	snapshot := newRemoteToolExecutor(ServerConfig{ID: "browser", ToolPrefix: "browser"}, nil, &sdk.Tool{Name: "browser_snapshot"}).Spec()

	if navigate.Effect != tools.EffectMutation {
		t.Fatalf("navigate Effect = %q, want %q", navigate.Effect, tools.EffectMutation)
	}
	if navigate.Risk != tools.RiskSafe {
		t.Fatalf("navigate Risk = %q, want %q", navigate.Risk, tools.RiskSafe)
	}
	if navigate.ApprovalMode != tools.ApprovalNever {
		t.Fatalf("navigate ApprovalMode = %q, want %q", navigate.ApprovalMode, tools.ApprovalNever)
	}
	if snapshot.Effect != tools.EffectReadOnly {
		t.Fatalf("snapshot Effect = %q, want %q", snapshot.Effect, tools.EffectReadOnly)
	}
	if snapshot.ApprovalMode != tools.ApprovalNever {
		t.Fatalf("snapshot ApprovalMode = %q, want %q", snapshot.ApprovalMode, tools.ApprovalNever)
	}
}

func TestNonBrowserMutationStillRequiresApproval(t *testing.T) {
	executor := newRemoteToolExecutor(ServerConfig{ID: "files", ToolPrefix: "files"}, nil, &sdk.Tool{Name: "write_file"})
	spec := executor.Spec()

	if spec.Effect != tools.EffectMutation {
		t.Fatalf("Effect = %q, want %q", spec.Effect, tools.EffectMutation)
	}
	if spec.Risk != tools.RiskApproval {
		t.Fatalf("Risk = %q, want %q", spec.Risk, tools.RiskApproval)
	}
	if spec.ApprovalMode != tools.ApprovalOnRequest {
		t.Fatalf("ApprovalMode = %q, want %q", spec.ApprovalMode, tools.ApprovalOnRequest)
	}
}

func TestToolInputSchemaCompactsJSONSchemaAnnotations(t *testing.T) {
	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"type":        "object",
		"description": "top-level annotation",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "verbose url annotation",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "parameter annotation, not parameter name",
			},
		},
	}

	raw := string(toolInputSchema(schema))
	if strings.Contains(raw, "verbose") || strings.Contains(raw, "$schema") || strings.Contains(raw, "top-level annotation") {
		t.Fatalf("schema annotations were not compacted: %s", raw)
	}
	if !strings.Contains(raw, `"description":{"type":"string"}`) {
		t.Fatalf("description parameter was removed instead of compacted: %s", raw)
	}
}
