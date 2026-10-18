package agent_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func openTodo() []todo.Item {
	return []todo.Item{
		{Content: "Read the parser", Status: todo.Completed},
		{Content: "Fix the bug", Status: todo.InProgress},
		{Content: "Run the tests", Status: todo.Pending},
	}
}

func TestOpenTodoItemsKeepTheRunGoingOnce(t *testing.T) {
	f := agenttest.NewFixture()
	f.Todos.Items = openTodo()
	model := agenttest.NewScriptedModel(text("The parser is read."), text("Stopping: the tests need a database I cannot reach."))
	task := f.Task(model)
	task.Continues = []string{"run_0"}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "Stopping: the tests need a database I cannot reach." {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(model.Requests()) != 2 || !reflect.DeepEqual(f.Todos.Chains[0], []string{agenttest.RunID, "run_0"}) {
		t.Fatalf("requests = %d, chains asked = %v", len(model.Requests()), f.Todos.Chains)
	}
	first := f.Journal.Messages[1]
	if first.Role != transcript.MessageRoleAssistant || first.Content != "The parser is read." || !hasFinish(first, "end_turn") {
		t.Fatalf("first reply = %+v", first)
	}
	nudge := lastMessage(model.Requests()[1])
	if nudge.Role != "user" || !strings.Contains(nudge.Content, "Your todo list has 2 items open") || !strings.Contains(nudge.Content, "1. [in_progress] Fix the bug\n2. [pending] Run the tests") {
		t.Fatalf("nudge = %+v", nudge)
	}
	if notes := engineNotes(f.Journal.Messages); len(notes) != 1 || notes[0].Origin != transcript.OriginEngineModel {
		t.Fatalf("engine notes = %+v", notes)
	}
	if !outcome.RunState.TodoNudged {
		t.Fatalf("state = %+v, want the nudge recorded", outcome.RunState)
	}
}

func TestReplyCompletesTheRunWithoutOpenTodoItems(t *testing.T) {
	for name, items := range map[string][]todo.Item{
		"no list":       nil,
		"all completed": {{Content: "Read the parser", Status: todo.Completed}},
	} {
		t.Run(name, func(t *testing.T) {
			f := agenttest.NewFixture()
			f.Todos.Items = items
			model := agenttest.NewScriptedModel(text("Done."))

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." || len(model.Requests()) != 1 {
				t.Fatalf("outcome = %+v, requests = %d", outcome, len(model.Requests()))
			}
		})
	}
}

func TestOpenTodoItemsDoNotHoldARunWithoutBudgetOrAfterTheNudge(t *testing.T) {
	for name, adjust := range map[string]func(*agent.Task){
		"budget spent":         func(task *agent.Task) { task.Budget = agent.Budget{Steps: 1} },
		"nudged before a park": func(task *agent.Task) { task.Resume = agent.RunState{TodoNudged: true} },
	} {
		t.Run(name, func(t *testing.T) {
			f := agenttest.NewFixture()
			f.Todos.Items = openTodo()
			model := agenttest.NewScriptedModel(text("Stopping here."))
			task := f.Task(model)
			adjust(&task)

			outcome := runTask(t, f, task)

			if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Stopping here." || len(model.Requests()) != 1 {
				t.Fatalf("outcome = %+v, requests = %d", outcome, len(model.Requests()))
			}
		})
	}
}

// toollessModel is a scripted model that cannot call tools.
type toollessModel struct {
	*agenttest.ScriptedModel
}

func (toollessModel) ModelCapabilities() providers.ModelCapabilities {
	return providers.ModelCapabilities{}
}

func TestOpenTodoItemsDoNotHoldARunWhoseModelCannotUseTools(t *testing.T) {
	f := agenttest.NewFixture()
	f.Todos.Items = openTodo()
	model := agenttest.NewScriptedModel(text("Stopping here."))

	outcome := runTask(t, f, f.Task(toollessModel{model}))

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Stopping here." || len(model.Requests()) != 1 {
		t.Fatalf("outcome = %+v, requests = %d", outcome, len(model.Requests()))
	}
}

// scriptedVerifier answers each Verify with the next verdict, then with none.
type scriptedVerifier struct {
	verdicts []agent.Verdict
	calls    int
}

func (v *scriptedVerifier) Verify(context.Context, string, string) (agent.Verdict, error) {
	v.calls++
	if len(v.verdicts) == 0 {
		return agent.Verdict{}, nil
	}
	verdict := v.verdicts[0]
	v.verdicts = v.verdicts[1:]
	return verdict, nil
}

func TestFailingGoalCheckKeepsTheRunGoingUntilItPasses(t *testing.T) {
	f := agenttest.NewFixture()
	f.Verifier = &scriptedVerifier{verdicts: []agent.Verdict{
		{Hold: true, Note: "Goal check failed (exit 1)."},
		{Note: "Verification passed."},
	}}
	model := agenttest.NewScriptedModel(text("Done, I think."), text("Fixed and done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Fixed and done." || len(model.Requests()) != 2 {
		t.Fatalf("outcome = %+v, requests = %d", outcome, len(model.Requests()))
	}
	if held := lastMessage(model.Requests()[1]); held.Role != "user" || held.Content != "Goal check failed (exit 1)." {
		t.Fatalf("held with %+v", held)
	}
	notes := engineNotes(f.Journal.Messages)
	if len(notes) != 2 || notes[0].Origin != transcript.OriginEngineModel || notes[1].Origin != transcript.OriginEngine || notes[1].Content != "Verification passed." {
		t.Fatalf("engine notes = %+v", notes)
	}
	if outcome.RunState.GoalHolds != 1 {
		t.Fatalf("state = %+v", outcome.RunState)
	}
}

func TestGoalChecksHoldARunBackABoundedNumberOfTimes(t *testing.T) {
	f := agenttest.NewFixture()
	verifier := &scriptedVerifier{}
	for range 10 {
		verifier.verdicts = append(verifier.verdicts, agent.Verdict{Hold: true, Note: "Goal check failed."})
	}
	f.Verifier = verifier
	turns := make([]agenttest.Turn, 10)
	for i := range turns {
		turns[i] = text("Stopping.")
	}
	model := agenttest.NewScriptedModel(turns...)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 6 || verifier.calls != 5 {
		t.Fatalf("outcome = %+v, requests = %d, checks = %d", outcome, len(model.Requests()), verifier.calls)
	}
}
