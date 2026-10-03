package clientruntime

import (
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func TestTaskEventsKeepTheSessionsSubagents(t *testing.T) {
	state := NewState(core.ClientSnapshot{SessionID: "s1"})
	for _, task := range []core.Task{
		{ID: "task_agent", SessionID: "s1", Kind: core.TaskKindSubagent, Status: core.TaskStatusRunning, AgentName: "Neo"},
		{ID: "task_shell", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning},
		{ID: "task_agent", SessionID: "s1", Kind: core.TaskKindSubagent, Status: core.TaskStatusCompleted, AgentName: "Neo"},
	} {
		payload, err := json.Marshal(task)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Apply(daemonclient.LiveEvent{Type: core.EventTaskUpdated, SessionID: "s1", Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}

	subagents := state.Snapshot().Subagents
	if len(subagents) != 1 || subagents[0].ID != "task_agent" || subagents[0].Status != core.TaskStatusCompleted {
		t.Fatalf("subagents = %+v", subagents)
	}
}
