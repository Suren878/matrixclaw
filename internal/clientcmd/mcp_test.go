package clientcmd

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type fakeToolClient struct{ status string }

func (fakeToolClient) Tools(context.Context) ([]tools.Spec, error) { return nil, nil }

func (f fakeToolClient) ExecuteTool(context.Context, core.ExecuteToolInput) (core.ExecuteToolResult, error) {
	return core.ExecuteToolResult{ToolResultMessage: &transcript.Message{
		Content: "out",
		Parts:   []transcript.MessagePart{{ToolResult: &transcript.ToolResultPart{Content: "out", Status: f.status}}},
	}}, nil
}

func TestMCPServeKeepsTheToolResultStatus(t *testing.T) {
	for status, want := range map[string]tools.ResultStatus{
		"":        tools.ResultStatusSuccess,
		"error":   tools.ResultStatusError,
		"neutral": tools.ResultStatusNeutral,
	} {
		runtime := &daemonToolRuntime{client: fakeToolClient{status: status}}
		result, err := runtime.Execute(context.Background(), "bash", tools.Call{})
		if err != nil || result.Status != want {
			t.Fatalf("status %q: result = %+v, err = %v; want %s", status, result, err, want)
		}
	}
}
