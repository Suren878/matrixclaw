package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// checkpointScenario is a full-auto session in a temporary directory whose
// write_file tool writes {"path","content"} there.
type checkpointScenario struct {
	app     *core.Core
	db      *store.SQLiteStore
	session core.Session
	dir     string
}

func newCheckpointScenario(t *testing.T, suffix string, script func(step int, request providers.Request) providers.Response) (*checkpointScenario, core.Run) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	db, cleanup := newCrashRecoveryStore(t)
	t.Cleanup(cleanup)
	app := core.New(core.Config{Store: db, Checkpoints: checkpoint.New(t.TempDir())})
	dir := t.TempDir()
	writeFile := funcTool{spec: tools.Spec{ID: "write_file", Description: "write", Effect: tools.EffectMutation, Namespace: "test", Category: tools.CategoryFilesystem, InputJSONSchema: []byte(`{"type":"object"}`)}, fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		var input struct{ Path, Content string }
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Content: "written"}, os.WriteFile(filepath.Join(dir, input.Path), []byte(input.Content), 0o644)
	}}
	inspect := funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: "inspected"}, nil
	}}
	app.SetTools(tools.NewRegistry(append(core.CheckpointToolExecutors(app), writeFile, inspect)...))
	step := 0
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		step++
		return script(step, request), nil
	})})
	session, run := saveCrashRecoveryRun(t, db, suffix, core.RunStatusAccepted, false)
	sessionIn(t, db, session, dir, core.PermissionModeFullAuto)
	session.WorkingDir = dir
	return &checkpointScenario{app: app, db: db, session: session, dir: dir}, run
}

func writeCall(id, path, content string) providers.ToolCall {
	return providers.ToolCall{ID: id, Name: "write_file", Arguments: []byte(`{"path":"` + path + `","content":"` + content + `"}`)}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFileCheckpointIsTakenOncePerBatchThatChangesFiles(t *testing.T) {
	t.Parallel()
	s, run := newCheckpointScenario(t, "checkpoint_batches", func(step int, _ providers.Request) providers.Response {
		switch step {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{writeCall("w1", "a.txt", "a1"), {ID: "r1", Name: "inspect_state", Arguments: []byte(`{}`)}, writeCall("w2", "b.txt", "b1")}}
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "r2", Name: "inspect_state", Arguments: []byte(`{}`)}}}
		case 3:
			return providers.Response{ToolCalls: []providers.ToolCall{writeCall("w3", "a.txt", "a2")}}
		default:
			return providers.Response{Text: "Done."}
		}
	})
	if err := os.WriteFile(filepath.Join(s.dir, "a.txt"), []byte("a0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	list, err := s.app.FileCheckpoints(context.Background(), s.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Checkpoints) != 2 {
		t.Fatalf("checkpoints = %d, want one per batch that writes (2)", len(list.Checkpoints))
	}
	first, second := list.Checkpoints[1], list.Checkpoints[0]
	if first.RunID != run.ID || first.ToolName != "write_file" {
		t.Fatalf("first checkpoint = %+v", first.FileCheckpoint)
	}
	// Restoring the first undoes both writes of its batch; the second only the last write.
	if len(first.Changes) != 2 || len(second.Changes) != 1 {
		t.Fatalf("changes: first %+v, second %+v", first.Changes, second.Changes)
	}
}

func TestUndoRestoresTheFilesBeforeTheRunAndTellsTheModel(t *testing.T) {
	t.Parallel()
	s, run := newCheckpointScenario(t, "checkpoint_undo", func(step int, _ providers.Request) providers.Response {
		switch step {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{writeCall("w1", "a.txt", "changed"), writeCall("w2", "new.txt", "new")}}
		default:
			return providers.Response{Text: "Done."}
		}
	})
	if err := os.WriteFile(filepath.Join(s.dir, "a.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.app.FileCheckpoints(context.Background(), s.session.ID)
	if err != nil || len(list.Checkpoints) != 1 {
		t.Fatalf("checkpoints = %+v, %v", list, err)
	}

	restored, err := s.app.RestoreFileCheckpoint(context.Background(), s.session.ID, list.Checkpoints[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, s.dir, "a.txt") != "original" {
		t.Fatal("a.txt was not restored")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file the run added must be deleted")
	}
	if len(restored.Changes) != 2 || restored.Note == nil || restored.Note.Origin != transcript.OriginEngine {
		t.Fatalf("restore = %+v", restored)
	}

	// The restore checkpointed what it replaced, so it can be undone too.
	list, err = s.app.FileCheckpoints(context.Background(), s.session.ID)
	if err != nil || len(list.Checkpoints) != 2 || list.Checkpoints[0].RunID != "" {
		t.Fatalf("after restore = %+v, %v", list, err)
	}
	if _, err := s.app.RestoreFileCheckpoint(context.Background(), s.session.ID, list.Checkpoints[0].ID); err != nil {
		t.Fatal(err)
	}
	if readFile(t, s.dir, "a.txt") != "changed" || readFile(t, s.dir, "new.txt") != "new" {
		t.Fatal("restoring the restore's checkpoint must bring the run's files back")
	}
}

func TestRestoreWaitsForTheActiveRun(t *testing.T) {
	t.Parallel()
	s, _ := newCheckpointScenario(t, "checkpoint_active", func(int, providers.Request) providers.Response {
		return providers.Response{Text: "Done."}
	})
	cp := core.FileCheckpoint{ID: "fcp_active", SessionID: s.session.ID, WorkingDir: s.dir, Tree: "4b825dc642cb6eb9a060e54bf8d69288fbee4904"}
	if err := s.db.CreateFileCheckpoint(context.Background(), cp); err != nil {
		t.Fatal(err)
	}
	// The scenario's run is accepted and has not ended.
	if _, err := s.app.RestoreFileCheckpoint(context.Background(), s.session.ID, cp.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("restore during a run = %v, want ErrRunActive", err)
	}
}

func TestCheckpointDiffShowsTheRunsWholeChange(t *testing.T) {
	t.Parallel()
	var results []string
	s, run := newCheckpointScenario(t, "checkpoint_diff", func(step int, request providers.Request) providers.Response {
		switch step {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "d0", Name: "checkpoint_diff", Arguments: []byte(`{}`)}}}
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{writeCall("w1", "a.txt", "one")}}
		case 3:
			return providers.Response{ToolCalls: []providers.ToolCall{writeCall("w2", "b.txt", "two")}}
		case 4:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "d1", Name: "checkpoint_diff", Arguments: []byte(`{"since":"run_start","path":"b.txt"}`)}}}
		default:
			results = toolResults(request)
			return providers.Response{Text: "Done."}
		}
	})
	if err := s.app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %q", results)
	}
	if strings.Contains(results[0], "b.txt") || !strings.Contains(results[3], "+two") || strings.Contains(results[3], "a.txt") {
		t.Fatalf("diff before any change = %q, diff of b.txt = %q", results[0], results[3])
	}
}
