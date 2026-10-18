package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func editCall(id, path string) providers.ToolCall {
	args, _ := json.Marshal(map[string]string{"file_path": path, "old_string": "a", "new_string": "b"})
	return providers.ToolCall{ID: id, Name: "edit", Arguments: args}
}

func bashCall(id, command string) providers.ToolCall {
	args, _ := json.Marshal(tools.BashParams{Command: command})
	return providers.ToolCall{ID: id, Name: "bash", Arguments: args}
}

// codingFixture has edit and a bash whose commands fail when they contain "fail".
func codingFixture() *agenttest.Fixture {
	f := agenttest.NewFixture()
	f.Tools.Funcs["edit"] = writeTool
	f.Tools.Funcs["bash"] = func(call tools.Call) tools.Result {
		var params tools.BashParams
		_ = json.Unmarshal(call.Args, &params)
		if strings.Contains(params.Command, "fail") {
			return tools.Result{Content: "FAIL", Status: tools.ResultStatusError, Metadata: tools.BashResponseMetadata{ExitCode: 1}}
		}
		return tools.Result{Content: "ok", Metadata: tools.BashResponseMetadata{}}
	}
	return f
}

func isVerifyNote(message providers.Message) bool {
	return message.Role == "user" && strings.Contains(message.Content, "build, test or lint")
}

func TestUnverifiedChangesAreSentBackOnce(t *testing.T) {
	f := codingFixture()
	model := agenttest.NewScriptedModel(
		calls(editCall("e1", "main.go")),
		text("Fixed."),
		calls(bashCall("b1", "cd /work && go test ./...")),
		text("Fixed; tests pass."),
	)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Fixed; tests pass." || len(requests) != 4 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if !isVerifyNote(lastMessage(requests[2])) {
		t.Fatalf("third request ends with %+v, want the verification note", lastMessage(requests[2]))
	}
	if !outcome.RunState.VerifyNudged || outcome.RunState.Unverified {
		t.Fatalf("state = %+v", outcome.RunState)
	}
}

func TestVerificationNoteIsSentOnlyOncePerRun(t *testing.T) {
	f := codingFixture()
	model := agenttest.NewScriptedModel(
		calls(editCall("e1", "main.go")),
		text("Fixed."),
		calls(editCall("e2", "util.go")),
		text("Cannot run the tests here: no toolchain."),
	)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Cannot run the tests here: no toolchain." || len(model.Requests()) != 4 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}

func TestNoVerificationNoteWhenNotNeeded(t *testing.T) {
	for _, tc := range []struct {
		name  string
		calls []providers.ToolCall
		bash  bool
	}{
		{"no changes", []providers.ToolCall{bashCall("b1", "ls")}, true},
		{"checked after the change", []providers.ToolCall{editCall("e1", "main.go"), bashCall("b1", "go build ./... && go vet ./...")}, true},
		{"package script", []providers.ToolCall{editCall("e1", "app.ts"), bashCall("b1", "pnpm run typecheck")}, true},
		{"docs only", []providers.ToolCall{editCall("e1", "README.md")}, true},
		{"no bash to check with", []providers.ToolCall{editCall("e1", "main.go")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := codingFixture()
			if !tc.bash {
				delete(f.Tools.Funcs, "bash")
			}
			model := agenttest.NewScriptedModel(calls(tc.calls...), text("Done."))

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." || len(model.Requests()) != 2 {
				t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
			}
		})
	}
}

func TestChangesNeedVerificationAgain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		calls []providers.ToolCall
	}{
		{"check before the change", []providers.ToolCall{bashCall("b1", "go test ./..."), editCall("e1", "main.go")}},
		{"failed check", []providers.ToolCall{editCall("e1", "main.go"), bashCall("b1", "go test ./... # fail")}},
		{"shell writes a file", []providers.ToolCall{bashCall("b1", "sed -i 's/a/b/' main.go")}},
		{"redirect into the tree", []providers.ToolCall{bashCall("b1", "cat > main.go <<'EOF'\npackage main\nEOF")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := codingFixture()
			model := agenttest.NewScriptedModel(calls(tc.calls...), text("Done."), text("Done, not verified."))

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 3 || !isVerifyNote(lastMessage(model.Requests()[2])) {
				t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
			}
		})
	}
}

func TestVerificationStateSurvivesAPark(t *testing.T) {
	f := codingFixture()
	f.Tools.Funcs["deploy"] = writeTool
	f.Tools.Asks["deploy"] = true
	model := agenttest.NewScriptedModel(calls(editCall("e1", "main.go"), call("d1", "deploy")))

	outcome := run(t, f, model)

	parked := f.Journal.States[len(f.Journal.States)-1].RunState
	if outcome.Status != agent.StatusWaitingApproval || !parked.Unverified {
		t.Fatalf("outcome = %+v parked checkpoint = %+v", outcome, parked)
	}
	f.Inbox.Grant("d1")
	resumed := agenttest.NewScriptedModel(text("Done."), text("Done, not verified."))
	task := f.Task(resumed)
	task.Resume = parked

	outcome = runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || len(resumed.Requests()) != 2 || !isVerifyNote(lastMessage(resumed.Requests()[1])) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(resumed.Requests()))
	}
}
