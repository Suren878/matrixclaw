package core_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type outputTool struct {
	spec    tools.Spec
	content string
}

func (t outputTool) Spec() tools.Spec { return t.spec }
func (t outputTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Content: t.content, Status: tools.ResultStatusSuccess}, nil
}

func bigToolOutput() string {
	return "FIRST LINE\n" + strings.Repeat("output line\n", 20_000) + "LAST LINE"
}

func assertKeptOutput(t *testing.T, root string, part *transcript.ToolResultPart, big string) {
	t.Helper()
	if part.OutputPath == "" || !strings.HasPrefix(part.OutputPath, root+string(filepath.Separator)) || !strings.Contains(part.Content, part.OutputPath) {
		t.Fatalf("result = %.200q path = %q", part.Content, part.OutputPath)
	}
	if !strings.Contains(part.Content, "FIRST LINE") || !strings.Contains(part.Content, "LAST LINE") || agentcontext.EstimateTextTokens(part.Content) > agentcontext.LargeOutputTokens {
		t.Fatalf("result keeps %d tokens without both ends", agentcontext.EstimateTextTokens(part.Content))
	}
	stored, err := os.ReadFile(part.OutputPath)
	if err != nil || string(stored) != big {
		t.Fatalf("stored output: %d bytes err = %v", len(stored), err)
	}
	info, err := os.Stat(part.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", info.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Dir(part.OutputPath))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", dir.Mode().Perm())
	}
}

func TestLargeToolOutputIsKeptInASessionFile(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	root := t.TempDir()
	big := bigToolOutput()
	app.WithSessionFiles(root)
	app.WithTools(tools.NewRegistry(outputTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), content: big}))
	session, _ := saveCrashRecoveryRun(t, db, "output", core.RunStatusCompleted, false)

	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "dump", Args: json.RawMessage(`{}`)})
	if err != nil || result.ToolResultMessage == nil {
		t.Fatalf("ExecuteTool = %+v err = %v", result, err)
	}
	part := result.ToolResultMessage.Parts[0].ToolResult
	assertKeptOutput(t, root, part, big)

	if err := app.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(part.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("output file after session delete: err = %v", err)
	}
}

func TestLargeToolOutputOfANativeRunIsKeptInASessionFile(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	root := t.TempDir()
	big := bigToolOutput()
	app.WithSessionFiles(root)
	app.WithTools(tools.NewRegistry(outputTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), content: big}))
	var seen string
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-dump", Name: "dump", Arguments: []byte(`{}`)}}}, nil
		}
		for _, message := range request.Messages {
			if message.ToolCallID == "call-dump" {
				seen = message.Content
			}
		}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "native_output", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	messages, err := db.ListMessages(context.Background(), session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var part *transcript.ToolResultPart
	for _, message := range messages {
		for _, p := range message.Parts {
			if p.ToolResult != nil && p.ToolResult.ToolCallID == "call-dump" {
				part = p.ToolResult
			}
		}
	}
	if part == nil {
		t.Fatalf("no stored result in %d messages", len(messages))
	}
	assertKeptOutput(t, root, part, big)
	if !strings.Contains(seen, part.OutputPath) {
		t.Fatalf("model saw %.200q", seen)
	}
}

func TestToolOutputOfAnUnsafeSessionIDStaysOutOfTheFileSystem(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	parent := t.TempDir()
	root := filepath.Join(parent, "sessions")
	app.WithSessionFiles(root)
	app.WithTools(tools.NewRegistry(outputTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), content: bigToolOutput()}))
	session, _ := saveCrashRecoveryRun(t, db, "output", core.RunStatusCompleted, false)
	session.ID = "../escape"
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}

	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "dump", Args: json.RawMessage(`{}`)})
	if err != nil || result.ToolResultMessage == nil {
		t.Fatalf("ExecuteTool = %+v err = %v", result, err)
	}
	part := result.ToolResultMessage.Parts[0].ToolResult
	if part.OutputPath != "" || agentcontext.EstimateTextTokens(part.Content) > agentcontext.LargeOutputTokens {
		t.Fatalf("path = %q, %d tokens", part.OutputPath, agentcontext.EstimateTextTokens(part.Content))
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("files written: %v err = %v", entries, err)
	}
	if err := app.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Fatalf("session delete touched the parent: %v", err)
	}
}

func TestSmallToolOutputStaysInline(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionFiles(t.TempDir())
	app.WithTools(tools.NewRegistry(outputTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), content: "small"}))
	session, _ := saveCrashRecoveryRun(t, db, "small_output", core.RunStatusCompleted, false)

	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "dump", Args: json.RawMessage(`{}`)})

	if err != nil || result.ToolResultMessage == nil || result.ToolResultMessage.Parts[0].ToolResult.Content != "small" || result.ToolResultMessage.Parts[0].ToolResult.OutputPath != "" {
		t.Fatalf("result = %+v err = %v", result.ToolResultMessage, err)
	}
}
