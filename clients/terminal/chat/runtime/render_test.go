package runtime

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var updateGoldens = flag.Bool("update", false, "rewrite the rendering goldens in testdata")

var renderBase = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// renderApp loads snapshot into a terminal of width x height and returns the
// screen as ANSI text.
func renderApp(t *testing.T, width int, height int, snapshot core.ClientSnapshot) (*appModel, string) {
	t.Helper()
	m := newApp(context.Background(), New(Config{Version: "1.0.0"}))
	m.now = renderBase.Add(42 * time.Second)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m.Update(connectedMsg{streamID: m.stream.id, sessionID: snapshot.SessionID})
	m.Update(snapshotMsg{streamID: m.stream.id, snapshot: snapshot})
	return m, m.viewContent()
}

// assertGolden compares a rendered screen with testdata/<name>.golden; run the
// tests with -update to rewrite it after an intended visual change.
func assertGolden(t *testing.T, name string, screen string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(screen), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if string(want) != screen {
		t.Fatalf("%s changed.\nwant:\n%s\n\ngot:\n%s", name, ansi.Strip(string(want)), ansi.Strip(screen))
	}
}

func renderSession() *core.Session {
	return &core.Session{ID: "session_1", Title: "Main", ProviderID: "openrouter", ModelID: "gpt-test", CreatedAt: renderBase, UpdatedAt: renderBase}
}

func renderContext() *core.ContextReport {
	return &core.ContextReport{SessionID: "session_1", Estimated: true, TokenEstimate: 12_345, WindowTokens: 200_000}
}

func at(seconds int) time.Time { return renderBase.Add(time.Duration(seconds) * time.Second) }

func textMessage(id string, role transcript.MessageRole, seconds int, text string) transcript.Message {
	return transcript.Message{ID: id, SessionID: "session_1", RunID: "run_1", Role: role, Content: text, Parts: transcript.NormalizeMessageParts(text, nil), CreatedAt: at(seconds), UpdatedAt: at(seconds)}
}

func toolCallMessage(id string, seconds int, callID string, name string, input any) transcript.Message {
	raw, _ := json.Marshal(input)
	return transcript.Message{ID: id, SessionID: "session_1", RunID: "run_1", Role: transcript.MessageRoleAssistant, CreatedAt: at(seconds), UpdatedAt: at(seconds), Parts: []transcript.MessagePart{
		{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: callID, Name: name, Input: string(raw), Finished: true}},
	}}
}

func toolResultMessage(id string, seconds int, callID string, name string, content string, metadata any) transcript.Message {
	var raw json.RawMessage
	if metadata != nil {
		raw, _ = json.Marshal(metadata)
	}
	return transcript.Message{ID: id, SessionID: "session_1", RunID: "run_1", Role: transcript.MessageRoleTool, CreatedAt: at(seconds), UpdatedAt: at(seconds), Parts: []transcript.MessagePart{
		{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: callID, Name: name, Content: content, Metadata: raw, Status: "success"}},
	}}
}

func finishedReply(id string, seconds int, text string) transcript.Message {
	message := textMessage(id, transcript.MessageRoleAssistant, seconds, text)
	message.Model, message.Provider = "gpt-test", "openrouter"
	message.Parts = append(message.Parts, transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "end_turn"}})
	return message
}

func TestRenderEmptySession(t *testing.T) {
	_, screen := renderApp(t, 100, 20, core.ClientSnapshot{SessionID: "session_1", Session: renderSession(), Context: renderContext()})
	assertGolden(t, "empty_session", screen)
}

func TestRenderTranscript(t *testing.T) {
	finished := at(70)
	snapshot := core.ClientSnapshot{
		SessionID: "session_1",
		Session:   renderSession(),
		Context:   renderContext(),
		Run:       &core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusCompleted, StartedAt: at(1), FinishedAt: &finished},
		Messages: []transcript.Message{
			textMessage("m01", transcript.MessageRoleUser, 0, "Fix the failing parser test and tell me what was wrong."),
			textMessage("m02", transcript.MessageRoleAssistant, 2, "Let me look at the **parser** first:\n\n- read the test\n- run it"),
			toolCallMessage("m03", 3, "call_read_1", "read", map[string]any{"file_path": "internal/parser/parse.go"}),
			toolResultMessage("m04", 4, "call_read_1", "read", "<file>\n1\tpackage parser\n</file>", map[string]any{"file_path": "internal/parser/parse.go"}),
			toolCallMessage("m05", 5, "call_read_2", "read", map[string]any{"file_path": "internal/parser/parse_test.go"}),
			toolResultMessage("m06", 6, "call_read_2", "read", "<file>\n1\tpackage parser\n</file>", map[string]any{"file_path": "internal/parser/parse_test.go"}),
			toolCallMessage("m07", 7, "call_bash", "bash", map[string]any{"command": "go test ./internal/parser/..."}),
			toolResultMessage("m08", 8, "call_bash", "bash", "--- FAIL: TestParse\n    parse_test.go:12: got 1, want 2\nFAIL", map[string]any{"output": "--- FAIL: TestParse\n    parse_test.go:12: got 1, want 2\nFAIL", "exit_code": 1}),
			toolCallMessage("m09", 9, "call_edit", "edit", map[string]any{"file_path": "internal/parser/parse.go", "old_string": "n := 1", "new_string": "n := 2"}),
			toolResultMessage("m10", 10, "call_edit", "edit", "edited", map[string]any{"file_path": "internal/parser/parse.go", "additions": 1, "removals": 1, "old_content": "n := 1\n", "new_content": "n := 2\n"}),
			toolCallMessage("m11", 11, "call_search", "web_search", map[string]any{"query": "go parser   off by one", "limit": 3}),
			toolResultMessage("m12", 12, "call_search", "web_search", "1. Off-by-one errors in parsers", nil),
			toolCallMessage("m13", 13, "call_agent", "agent", map[string]any{"description": "Check callers", "prompt": "Find other callers of Parse"}),
			toolResultMessage("m14", 60, "call_agent", "agent", "Two callers, both fine.", nil),
			finishedReply("m15", 70, "The loop started at `1` instead of `2`. Fixed and the test passes."),
		},
		Subagents: []core.Task{{ID: "task_1", Kind: core.TaskKindSubagent, AgentName: "scout", Description: "Check callers", SessionID: "session_1", RunID: "run_1", ParentToolCallID: "call_agent", Command: "Find other callers of Parse", Status: core.TaskStatusCompleted, Summary: "Two callers, both fine.", StartedAt: at(13), UpdatedAt: at(60)}},
	}
	_, screen := renderApp(t, 110, 70, snapshot)
	assertGolden(t, "transcript", screen)
}

func TestRenderWorkingWithTodoPanel(t *testing.T) {
	snapshot := core.ClientSnapshot{
		SessionID: "session_1",
		Session:   renderSession(),
		Context:   renderContext(),
		Run:       &core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusRunning, StartedAt: at(0)},
		Messages: []transcript.Message{
			textMessage("m01", transcript.MessageRoleUser, 0, "Summarise https://example.com/post"),
			toolCallMessage("m02", 2, "call_fetch", "web_fetch", map[string]any{"url": "https://example.com/post"}),
		},
		ToolUpdates:   []core.ToolUpdate{{ToolCallID: "call_fetch", ToolName: "web_fetch", State: core.ToolLifecycleRequested, RunID: "run_1", SessionID: "session_1"}},
		PendingInputs: []core.SessionInput{{ID: "input_1", SessionID: "session_1", Mode: core.BusyInputModeQueue, Status: core.SessionInputStatusPending, CreatedAt: at(5)}},
		Todo: &todo.List{SessionID: "session_1", Items: []todo.Item{
			{Content: "Fetch the post", ActiveForm: "Fetching the post", Status: todo.InProgress},
			{Content: "Write the summary", Status: todo.Pending},
		}},
	}
	_, screen := renderApp(t, 160, 34, snapshot)
	assertGolden(t, "working_todo", screen)
}

func TestRenderPermissionDialog(t *testing.T) {
	snapshot := core.ClientSnapshot{
		SessionID: "session_1",
		Session:   renderSession(),
		Context:   renderContext(),
		Run:       &core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusWaitingApproval, StartedAt: at(0)},
		Messages: []transcript.Message{
			textMessage("m01", transcript.MessageRoleUser, 0, "Clean the build cache"),
			toolCallMessage("m02", 2, "call_bash", "bash", map[string]any{"command": "rm -rf ./build"}),
		},
		Approvals: []core.Approval{{ID: "approval_1", SessionID: "session_1", RunID: "run_1", ToolCallRef: "call_bash", ToolName: "bash", Action: "execute", Description: "Run a shell command", Params: json.RawMessage(`{"command":"rm -rf ./build"}`), State: core.ApprovalStatePending}},
	}
	m, screen := renderApp(t, 110, 30, snapshot)
	if !strings.Contains(ansi.Strip(screen), "rm -rf ./build") || !m.dialog.HasDialogs() {
		t.Fatalf("permission dialog missing:\n%s", ansi.Strip(screen))
	}
	assertGolden(t, "permission_dialog", screen)
}

func TestRenderCommandsDialog(t *testing.T) {
	m, _ := renderApp(t, 110, 34, core.ClientSnapshot{SessionID: "session_1", Session: renderSession(), Context: renderContext()})
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !m.dialog.HasDialogs() {
		t.Fatal("ctrl+p did not open the commands dialog")
	}
	assertGolden(t, "commands_dialog", m.viewContent())
}

// deliver hands a live event to the app as its event stream would.
func deliver(t *testing.T, m *appModel, eventType core.EventType, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(liveEventMsg{streamID: m.stream.id, event: daemonclient.LiveEvent{Type: eventType, SessionID: "session_1", Payload: raw}})
}
