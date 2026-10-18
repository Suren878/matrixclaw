package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const checkpointDiffToolName = "checkpoint_diff"

// sinceRunStart names the run's first checkpoint in checkpoint_diff.
const sinceRunStart = "run_start"

var checkpointDiffToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "since": {"type": "string", "description": "run_start (default): the state before this run first changed files; or a checkpoint id"},
    "path": {"type": "string", "description": "Limit the diff to this file or directory"}
  },
  "additionalProperties": false
}`)

// CheckpointToolExecutors returns checkpoint_diff while file checkpoints are on.
func CheckpointToolExecutors(app *Core) []tools.Executor {
	if app == nil || app.checkpoints == nil {
		return nil
	}
	return []tools.Executor{&checkpointDiffTool{app: app}}
}

type checkpointDiffTool struct {
	app *Core
}

func (t *checkpointDiffTool) Spec() tools.Spec {
	return tools.Spec{
		ID: checkpointDiffToolName,
		Description: "Show every file change in the working directory since this run started (or since a checkpoint id): a summary of changed files, then a unified diff. " +
			"It covers changes made by edit tools and by shell commands alike. Use it to review your whole change before you finish.",
		Effect:          tools.EffectReadOnly,
		Namespace:       "core.checkpoint",
		Category:        tools.CategoryFilesystem,
		InputJSONSchema: checkpointDiffToolSchema,
	}
}

func (t *checkpointDiffTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Since string `json:"since"`
		Path  string `json:"path"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{}, tools.InvalidArgs(checkpointDiffToolName, err)
		}
	}
	session, err := t.app.store.GetSession(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	base, found, err := t.app.diffBase(ctx, session, call.RunID, strings.TrimSpace(input.Since))
	if err != nil {
		return tools.Result{}, err
	}
	if !found {
		return tools.Result{Content: "No file changes recorded in this run yet: a checkpoint is taken before the first tool call that changes files.", Status: tools.ResultStatusNeutral}, nil
	}
	var paths []string
	if p := strings.TrimSpace(input.Path); p != "" {
		paths = []string{p}
	}
	diff, err := t.app.checkpoints.Diff(ctx, base.WorkingDir, base.Tree, "", paths...)
	if errors.Is(err, checkpoint.ErrUnsupported) || errors.Is(err, checkpoint.ErrMissing) {
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	if err != nil {
		return tools.Result{}, err
	}
	header := fmt.Sprintf("Changes since checkpoint %s (taken %s, before %s)", base.ID, base.CreatedAt.Format(time.TimeOnly+" UTC"), base.ToolName)
	if len(diff.Changes) == 0 {
		return tools.Result{Content: header + ": none.", Status: tools.ResultStatusNeutral}, nil
	}
	return tools.Result{Content: header + ":\n" + DiffStat(diff.Changes) + "\n\n" + diff.Patch, Metadata: diff.Changes}, nil
}

// diffBase finds the checkpoint a diff starts from: the run's first one, or
// the one with the given id taken in this session or its parent, in its
// working directory.
func (c *Core) diffBase(ctx context.Context, session Session, runID, since string) (FileCheckpoint, bool, error) {
	if since != "" && since != sinceRunStart {
		cp, err := c.store.GetFileCheckpoint(ctx, since)
		if errors.Is(err, ErrNotFound) || (err == nil && (cp.WorkingDir != session.WorkingDir || (cp.SessionID != session.ID && cp.SessionID != session.ParentSessionID))) {
			return FileCheckpoint{}, false, fmt.Errorf("%w: no checkpoint %q in this session", ErrInvalidInput, since)
		}
		return cp, err == nil, err
	}
	if runID == "" {
		return FileCheckpoint{}, false, nil
	}
	list, err := c.store.ListFileCheckpoints(ctx, session.ID, 0)
	if err != nil {
		return FileCheckpoint{}, false, err
	}
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].RunID == runID {
			return list[i], true, nil
		}
	}
	return FileCheckpoint{}, false, nil
}

// DiffStat renders changes as one line per file and a total, like git's diffstat.
func DiffStat(changes []checkpoint.Change) string {
	var b strings.Builder
	added, deleted := 0, 0
	for _, change := range changes {
		counts := fmt.Sprintf("+%d -%d", change.Added, change.Deleted)
		if change.Binary {
			counts = "binary"
		}
		fmt.Fprintf(&b, "%-8s %s (%s)\n", change.Status, change.Path, counts)
		added += change.Added
		deleted += change.Deleted
	}
	fmt.Fprintf(&b, "%d files changed, +%d -%d", len(changes), added, deleted)
	return b.String()
}
