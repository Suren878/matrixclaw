package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestRunWakeupsComeDueInOrder(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	for _, id := range []string{"r1", "r2", "r3"} {
		createTestRun(t, st, "s1", id)
	}
	for _, wakeup := range []core.RunWakeup{
		{RunID: "r1", SessionID: "s1", WakeAt: testEpoch.Add(2 * time.Minute), TaskIDs: []string{"task_a", "task_b"}},
		{RunID: "r2", SessionID: "s1", WakeAt: testEpoch.Add(time.Minute)},
		{RunID: "r3", SessionID: "s1", WakeAt: testEpoch.Add(time.Hour)},
		{RunID: "r1", SessionID: "s1", WakeAt: testEpoch.Add(90 * time.Second), TaskIDs: []string{"task_a"}},
	} {
		if err := st.SaveRunWakeup(ctx, wakeup); err != nil {
			t.Fatal(err)
		}
	}

	due, err := st.ListDueRunWakeups(ctx, testEpoch.Add(2*time.Minute))
	if err != nil || len(due) != 2 || due[0].RunID != "r2" || due[1].RunID != "r1" || !reflect.DeepEqual(due[1].TaskIDs, []string{"task_a"}) || !due[1].WakeAt.Equal(testEpoch.Add(90*time.Second)) {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if err := st.DeleteRunWakeup(ctx, "r2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRunWakeup(ctx, "r2"); err != core.ErrNotFound {
		t.Fatalf("deleted wakeup err = %v", err)
	}
	if got, err := st.GetRunWakeup(ctx, "r3"); err != nil || got.TaskIDs != nil || got.SessionID != "s1" {
		t.Fatalf("r3 = %+v, %v", got, err)
	}
}

func TestWaitingRunsAreActiveAndSessionRunsListNewestFirst(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	for i, id := range []string{"r1", "r2", "r3"} {
		at := testEpoch.Add(time.Duration(i) * time.Minute)
		message := transcript.Message{ID: "m_" + id, SessionID: "s1", RunID: id, Role: transcript.MessageRoleUser, Content: id, CreatedAt: at, UpdatedAt: at}
		run := core.Run{ID: id, SessionID: "s1", UserMessageID: message.ID, Status: core.RunStatusCompleted, Trigger: core.RunTriggerWake, StartedAt: at, UpdatedAt: at}
		if id == "r3" {
			run.Status = core.RunStatusWaitingEvents
		}
		if err := st.AcceptMessage(ctx, message, run); err != nil {
			t.Fatal(err)
		}
	}

	active, err := st.GetActiveRunBySession(ctx, "s1")
	if err != nil || active.ID != "r3" {
		t.Fatalf("active = %+v, %v", active, err)
	}
	all, err := st.ListActiveRuns(ctx)
	if err != nil || len(all) != 1 || all[0].ID != "r3" {
		t.Fatalf("active runs = %+v, %v", all, err)
	}
	runs, err := st.ListSessionRuns(ctx, "s1", 2)
	if err != nil || len(runs) != 2 || runs[0].ID != "r3" || runs[1].ID != "r2" || runs[1].Trigger != core.RunTriggerWake {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}
