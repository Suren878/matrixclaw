package controlplane

import (
	"net/http"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/core"
)

func checkpointDaemon(t *testing.T) (*fakeDaemon, *[]string) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	view := func(id, runID string, files int) core.FileCheckpointView {
		changes := make([]checkpoint.Change, files)
		for i := range changes {
			changes[i] = checkpoint.Change{Path: "f.go", Status: checkpoint.StatusModified, Added: 1}
		}
		return core.FileCheckpointView{FileCheckpoint: core.FileCheckpoint{ID: id, SessionID: "s1", RunID: runID, ToolName: "edit", CreatedAt: at}, Changes: changes}
	}
	var restored []string
	daemon := newFakeDaemon(t).
		on("GET /v1/sessions/{id}/checkpoints", func(*http.Request) any {
			// Newest first: the last run took two, an earlier run one.
			return core.FileCheckpointsResponse{Checkpoints: []core.FileCheckpointView{view("c3", "run2", 1), view("c2", "run2", 2), view("c1", "run1", 3)}}
		}).
		on("POST /v1/sessions/{id}/checkpoints/{checkpoint}/restore", func(r *http.Request) any {
			restored = append(restored, r.PathValue("checkpoint"))
			return core.FileCheckpointRestoreResponse{Checkpoint: view(r.PathValue("checkpoint"), "run2", 0).FileCheckpoint, Changes: view("", "", 2).Changes}
		})
	return daemon, &restored
}

func TestUndoConfirmsAndRestoresTheLastRunsFirstCheckpoint(t *testing.T) {
	daemon, restored := checkpointDaemon(t)

	result := daemon.run("/undo")
	if result.Confirm == nil || result.Confirm.ConfirmCommand != "/undo confirm c2" {
		t.Fatalf("undo = %+v", result)
	}
	if len(*restored) != 0 {
		t.Fatal("undo restored before the confirmation")
	}
	result = daemon.run(result.Confirm.ConfirmCommand)
	if len(*restored) != 1 || (*restored)[0] != "c2" || !result.ReloadSnapshot {
		t.Fatalf("confirm = %+v, restored %v", result, *restored)
	}
}

func TestRewindPicksAnyCheckpoint(t *testing.T) {
	daemon, restored := checkpointDaemon(t)

	picker := daemon.run("/rewind").Picker
	if picker == nil || picker.Kind != PickerCheckpoints || len(picker.Items) != 3 {
		t.Fatalf("picker = %+v", picker)
	}
	confirm := daemon.run(picker.Items[2].Command).Confirm
	if confirm == nil || confirm.ConfirmCommand != "/rewind confirm c1" {
		t.Fatalf("confirm = %+v", confirm)
	}
	daemon.run(confirm.ConfirmCommand)
	if len(*restored) != 1 || (*restored)[0] != "c1" {
		t.Fatalf("restored %v", *restored)
	}
}

func TestOnlyTheOwnerRestoresFiles(t *testing.T) {
	daemon, restored := checkpointDaemon(t)
	for _, command := range []string{"/undo", "/rewind confirm c1"} {
		if result := daemon.runAs(core.RoleMember, command); result.Confirm != nil || result.Picker != nil {
			t.Fatalf("%s as member = %+v", command, result)
		}
	}
	if len(*restored) != 0 {
		t.Fatal("a member restored files")
	}
}
