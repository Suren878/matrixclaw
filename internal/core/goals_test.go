package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// goalScenario is a session whose model answers the nth request (from 0) with
// reply(ctx, n); it has the todo and goal tools.
type goalScenario struct {
	app      *core.Core
	db       *store.SQLiteStore
	session  core.Session
	starter  *executingRunStarter
	mu       sync.Mutex
	requests []providers.Request
}

func newGoalScenario(t *testing.T, reply func(ctx context.Context, n int) (providers.Response, error)) *goalScenario {
	t.Helper()
	db := openScenarioStore(t)
	app := core.New(db)
	s := &goalScenario{app: app, db: db, starter: &executingRunStarter{app: app}}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(s.starter)
	app.WithTools(tools.NewRegistry(append(core.TodoToolExecutors(app), core.GoalToolExecutors(app)...)...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		s.mu.Lock()
		n := len(s.requests)
		s.requests = append(s.requests, request)
		s.mu.Unlock()
		return reply(ctx, n)
	})})
	s.session = permissionSession(t, db, "session_goal", t.TempDir(), core.PermissionModeDefault, "")
	return s
}

func (s *goalScenario) setGoal(t *testing.T, input core.GoalInput) {
	t.Helper()
	if _, err := s.app.SetSessionGoal(context.Background(), s.session.ID, input); err != nil {
		t.Fatal(err)
	}
	s.starter.wait(t)
}

func (s *goalScenario) goal(t *testing.T) core.SessionGoal {
	t.Helper()
	goal, err := s.db.GetSessionGoal(context.Background(), s.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	return goal
}

// runs lists the session's runs, oldest first.
func (s *goalScenario) runs(t *testing.T) []core.Run {
	t.Helper()
	runs, err := s.db.ListSessionRuns(context.Background(), s.session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}
	return runs
}

func (s *goalScenario) messages(t *testing.T) []transcript.Message {
	t.Helper()
	messages, err := s.db.ListMessages(context.Background(), s.session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func (s *goalScenario) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *goalScenario) lastUserText(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages := s.requests[n].Messages
	return messages[len(messages)-1].Content
}

// replies answers the nth request with reply(n).
func replies(reply func(n int) providers.Response) func(context.Context, int) (providers.Response, error) {
	return func(_ context.Context, n int) (providers.Response, error) { return reply(n), nil }
}

// requestSees reports whether the nth request sends text in a message and
// offers tool.
func (s *goalScenario) requestSees(n int, text, tool string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sent, offered := false, false
	for _, message := range s.requests[n].Messages {
		sent = sent || strings.Contains(message.Content, text)
	}
	for _, definition := range s.requests[n].Tools {
		offered = offered || definition.Name == tool
	}
	return sent && offered
}

func textReply(text string) providers.Response {
	return providers.Response{Text: text}
}

func toolReply(id, name, args string) providers.Response {
	return providers.Response{ToolCalls: []providers.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}}
}

// toolResult is the content of the result of call id.
func toolResult(t *testing.T, messages []transcript.Message, id string) (string, bool) {
	t.Helper()
	for _, message := range messages {
		for _, part := range message.Parts {
			if result := part.ToolResult; result != nil && result.ToolCallID == id {
				return message.Content, result.IsError()
			}
		}
	}
	t.Fatalf("no result for %s", id)
	return "", false
}

func hasMessage(messages []transcript.Message, role transcript.MessageRole, prefix string) bool {
	for _, message := range messages {
		if message.Role == role && strings.HasPrefix(message.Content, prefix) {
			return true
		}
	}
	return false
}

func TestFailingGoalCheckKeepsTheRunGoingAndRunsStopAtTheirAllowance(t *testing.T) {
	t.Parallel()
	s := newGoalScenario(t, replies(func(int) providers.Response { return textReply("Done.") }))
	// Fails once, then passes.
	s.setGoal(t, core.GoalInput{Objective: "Make it pass", CheckCommand: "test -f ran || { touch ran; echo not yet; exit 3; }", MaxRuns: 1})

	runs := s.runs(t)
	if len(runs) != 1 || runs[0].Trigger != core.RunTriggerGoal || runs[0].Status != core.RunStatusCompleted || s.requestCount() != 2 {
		t.Fatalf("runs = %+v, requests = %d", runs, s.requestCount())
	}
	if held := s.lastUserText(1); !strings.Contains(held, "exit 3") || !strings.Contains(held, "not yet") {
		t.Fatalf("the run went on with %q", held)
	}
	goal := s.goal(t)
	if goal.Status != core.GoalPaused || goal.RunsUsed != 1 || goal.LastCheck != "passed" || goal.LastRunID != runs[0].ID {
		t.Fatalf("goal = %+v", goal)
	}
	messages := s.messages(t)
	if !hasMessage(messages, transcript.MessageRoleSystem, "Verification passed") || !hasMessage(messages, transcript.MessageRoleSystem, "The goal is paused") {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestTheSameCheckFailureThreeTimesBlocksTheGoal(t *testing.T) {
	t.Parallel()
	s := newGoalScenario(t, replies(func(int) providers.Response { return textReply("Done.") }))
	s.setGoal(t, core.GoalInput{Objective: "Make it pass", CheckCommand: "echo FAIL after $RANDOM ms; exit 1", MaxRuns: 5})

	if runs := s.runs(t); len(runs) != 1 || s.requestCount() != 3 {
		t.Fatalf("runs = %+v, requests = %d", runs, s.requestCount())
	}
	goal := s.goal(t)
	if goal.Status != core.GoalBlocked || goal.CheckFailures != 3 || goal.RunsUsed != 1 {
		t.Fatalf("goal = %+v", goal)
	}
}

func TestGoalRunsContinueEachOtherUntilTheirAllowanceIsUsed(t *testing.T) {
	t.Parallel()
	s := newGoalScenario(t, replies(func(int) providers.Response { return textReply("Still working.") }))
	s.setGoal(t, core.GoalInput{Objective: "Port the parser", MaxRuns: 3})

	runs := s.runs(t)
	if len(runs) != 3 {
		t.Fatalf("runs = %+v", runs)
	}
	for i, run := range runs {
		if run.Trigger != core.RunTriggerGoal || run.Status != core.RunStatusCompleted || i > 0 && run.ContinuesRunID != runs[i-1].ID {
			t.Fatalf("run %d = %+v", i, run)
		}
	}
	if goal := s.goal(t); goal.Status != core.GoalPaused || goal.RunsUsed != 3 {
		t.Fatalf("goal = %+v", goal)
	}
	if !s.requestSees(0, "Port the parser", "goal_update") {
		t.Fatal("a goal run does not see the objective and the goal tools")
	}

	if _, err := s.app.ChangeSessionGoal(context.Background(), s.session.ID, core.GoalResume); err != nil {
		t.Fatal(err)
	}
	s.starter.wait(t)
	if runs := s.runs(t); len(runs) != 6 {
		t.Fatalf("a resumed goal ran %d runs in all", len(runs))
	}
}

func TestQueuedUserMessageRunsBeforeTheNextGoalRun(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	s := newGoalScenario(t, replies(func(n int) providers.Response {
		if n == 0 {
			once.Do(func() { close(started) })
			<-release
		}
		return textReply("Working.")
	}))
	ctx := context.Background()
	if _, err := s.app.SetSessionGoal(ctx, s.session.ID, core.GoalInput{Objective: "Port the parser", MaxRuns: 2}); err != nil {
		t.Fatal(err)
	}
	<-started
	accepted, err := s.app.AcceptRun(ctx, core.HandleMessageInput{SessionID: s.session.ID, Client: "terminal", ExternalKey: "local", Text: "How is it going?", BusyMode: core.BusyInputModeQueue})
	if err != nil || accepted.Status != core.AcceptRunStatusQueued {
		t.Fatalf("accepted = %+v, %v", accepted, err)
	}
	close(release)
	s.starter.wait(t)

	runs := s.runs(t)
	if len(runs) != 3 || runs[0].Trigger != core.RunTriggerGoal || runs[1].Trigger != "" || runs[2].Trigger != core.RunTriggerGoal {
		t.Fatalf("runs = %+v", runs)
	}
	if goal := s.goal(t); goal.Status != core.GoalPaused || goal.RunsUsed != 2 {
		t.Fatalf("goal = %+v", goal)
	}
}

func TestCancelingAGoalRunPausesTheGoal(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	s := newGoalScenario(t, func(ctx context.Context, n int) (providers.Response, error) {
		if n == 0 {
			close(started)
		}
		<-ctx.Done()
		return providers.Response{}, ctx.Err()
	})
	ctx := context.Background()
	if _, err := s.app.SetSessionGoal(ctx, s.session.ID, core.GoalInput{Objective: "Port the parser"}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := s.app.CancelRun(ctx, s.runs(t)[0].ID); err != nil {
		t.Fatal(err)
	}
	s.starter.wait(t)

	if runs := s.runs(t); len(runs) != 1 || runs[0].Status != core.RunStatusCanceled {
		t.Fatalf("runs = %+v", runs)
	}
	if goal := s.goal(t); goal.Status != core.GoalPaused || goal.RunsUsed != 1 {
		t.Fatalf("goal = %+v", goal)
	}
}

func TestBlockedIsAcceptedAfterThreeConsecutiveGoalRunsReportIt(t *testing.T) {
	t.Parallel()
	const blocker = `{"status":"blocked","blocker":"The staging database refuses connections."}`
	s := newGoalScenario(t, replies(func(n int) providers.Response {
		if n%2 == 0 {
			return toolReply("call_block_"+string(rune('a'+n/2)), "goal_update", blocker)
		}
		return textReply("Blocked on the database.")
	}))
	s.setGoal(t, core.GoalInput{Objective: "Migrate staging", MaxRuns: 10})

	if runs := s.runs(t); len(runs) != 3 {
		t.Fatalf("runs = %+v", runs)
	}
	messages := s.messages(t)
	for id, want := range map[string]string{"call_block_a": "1 of 3", "call_block_b": "2 of 3", "call_block_c": "blocked"} {
		if content, failed := toolResult(t, messages, id); failed || !strings.Contains(content, want) {
			t.Fatalf("%s = %q (error %v), want %q", id, content, failed, want)
		}
	}
	if goal := s.goal(t); goal.Status != core.GoalBlocked || goal.BlockerRepeats != 3 || goal.Reason != "The staging database refuses connections." {
		t.Fatalf("goal = %+v", goal)
	}
}

func TestGoalCompletesOnlyWithEvidenceNoOpenTodoAndAPassingCheck(t *testing.T) {
	t.Parallel()
	steps := []providers.Response{
		toolReply("call_plan", "todo_write", `{"items":[{"content":"Port the parser","status":"in_progress"}]}`),
		toolReply("call_open", "goal_update", `{"status":"complete","evidence":"ported"}`),
		toolReply("call_done", "todo_write", `{"items":[{"content":"Port the parser","status":"completed"}]}`),
		toolReply("call_bare", "goal_update", `{"status":"complete"}`),
		toolReply("call_failing", "goal_update", `{"status":"complete","evidence":"ported"}`),
		toolReply("call_passing", "goal_update", `{"status":"complete","evidence":"ported; the check passes"}`),
		textReply("The parser is ported."),
	}
	s := newGoalScenario(t, replies(func(n int) providers.Response { return steps[min(n, len(steps)-1)] }))
	s.setGoal(t, core.GoalInput{Objective: "Port the parser", CheckCommand: "test -f ran || { touch ran; exit 1; }"})

	messages := s.messages(t)
	for id, wantError := range map[string]bool{"call_open": true, "call_bare": true, "call_failing": true, "call_passing": false} {
		if _, failed := toolResult(t, messages, id); failed != wantError {
			t.Fatalf("%s error = %v, want %v", id, failed, wantError)
		}
	}
	goal := s.goal(t)
	if goal.Status != core.GoalComplete || goal.Reason != "ported; the check passes" || goal.RunsUsed != 1 {
		t.Fatalf("goal = %+v", goal)
	}
	if runs := s.runs(t); len(runs) != 1 || s.requestCount() != len(steps) {
		t.Fatalf("runs = %+v, requests = %d", runs, s.requestCount())
	}
}
