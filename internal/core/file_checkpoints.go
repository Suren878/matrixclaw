package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// keepFileCheckpoints is how many checkpoints each working directory keeps.
const keepFileCheckpoints = 50

// snapshotTimeout bounds one snapshot; a slower one is skipped.
const snapshotTimeout = time.Minute

// restoreToolName labels the checkpoint a restore takes of the state it replaces.
const restoreToolName = "restore"

// WithCheckpoints snapshots working directories into store before tool
// batches that change files; nil (the default) takes none.
func (c *Core) WithCheckpoints(store *checkpoint.Store) *Core {
	c.checkpoints = store
	return c
}

// batchSnapshot is shared by the Journal and Tools ports of one run: the
// engine's checkpoint before a model call starts a new batch, and the batch's
// first call that may change files takes its file checkpoint.
type batchSnapshot struct {
	taken atomic.Bool
}

func (b *batchSnapshot) next() {
	if b != nil {
		b.taken.Store(false)
	}
}

// changesFiles reports whether a call may change files in the run's working
// directory: a mutating file or shell tool, a child agent writing it, or a
// worktree merge.
func (c *Core) changesFiles(spec tools.Spec, call tools.Call) bool {
	switch spec.ID {
	case agentToolName:
		return c.agentCallWrites(call)
	case worktreeMergeToolName:
		return true
	}
	return spec.Mutates() && (spec.Category == tools.CategoryFilesystem || spec.Category == tools.CategoryShell)
}

// checkpointBatch takes the batch's file checkpoint once, before its first
// call that may change files starts. A failure is logged and the run goes on.
func (t coreTools) checkpointBatch(ctx context.Context, spec tools.Spec, call tools.Call) {
	if t.c.checkpoints == nil || t.batch == nil || t.turn.Readonly || !t.c.changesFiles(spec, call) || t.batch.taken.Swap(true) {
		return
	}
	if _, err := t.c.takeFileCheckpoint(ctx, t.turn.SessionID, t.turn.RunID, t.turn.WorkingDir, spec.ID); err != nil {
		t.c.logCheckpointFailure(t.turn.WorkingDir, err)
	}
}

// takeFileCheckpoint snapshots workingDir and records it for the session,
// unless the run's newest checkpoint already holds the same tree.
func (c *Core) takeFileCheckpoint(ctx context.Context, sessionID, runID, workingDir, toolName string) (FileCheckpoint, error) {
	snapCtx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	tree, err := c.checkpoints.Snapshot(snapCtx, workingDir)
	if err != nil {
		return FileCheckpoint{}, err
	}
	newest, err := c.store.ListFileCheckpoints(ctx, sessionID, 1)
	if err != nil {
		return FileCheckpoint{}, err
	}
	if runID != "" && len(newest) == 1 && newest[0].RunID == runID && newest[0].Tree == tree {
		return newest[0], nil
	}
	var seq int64
	if latest, err := c.store.ListMessages(ctx, sessionID, 1); err != nil {
		return FileCheckpoint{}, err
	} else if len(latest) > 0 {
		seq = latest[0].Seq
	}
	cp := FileCheckpoint{ID: c.newID("fcp"), SessionID: sessionID, RunID: runID, Seq: seq, ToolName: toolName, WorkingDir: workingDir, Tree: tree, CreatedAt: c.now().UTC()}
	if err := c.store.CreateFileCheckpoint(ctx, cp); err != nil {
		return FileCheckpoint{}, err
	}
	trimmed, err := c.store.TrimFileCheckpoints(ctx, workingDir, keepFileCheckpoints)
	if err != nil {
		return FileCheckpoint{}, err
	}
	if trimmed {
		c.pruneCheckpoints(workingDir)
	}
	return cp, nil
}

// pruneCheckpoints drops, in the background, the snapshots of workingDir no
// checkpoint keeps.
func (c *Core) pruneCheckpoints(workingDir string) {
	safego.Go("core.pruneCheckpoints", func() {
		ctx, cancel := context.WithTimeout(c.lifetime, 10*time.Minute)
		defer cancel()
		trees, err := c.store.FileCheckpointTrees(ctx, workingDir)
		if err == nil {
			err = c.checkpoints.Prune(ctx, workingDir, trees)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("core: prune checkpoints of %s: %v", workingDir, err)
		}
	})
}

// logCheckpointFailure logs why a working directory is not snapshotted, once
// per directory and reason.
func (c *Core) logCheckpointFailure(workingDir string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if _, logged := c.checkpointFailures.LoadOrStore(workingDir+"\x00"+err.Error(), true); !logged {
		log.Printf("core: no file checkpoint for %s: %v", workingDir, err)
	}
}

// FileCheckpoints lists the session's checkpoints, newest first, with what
// restoring each would change now.
func (c *Core) FileCheckpoints(ctx context.Context, sessionID string) (FileCheckpointsResponse, error) {
	session, err := c.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return FileCheckpointsResponse{}, err
	}
	if c.checkpoints == nil {
		return FileCheckpointsResponse{Checkpoints: []FileCheckpointView{}, Unavailable: "File checkpoints are off (daemon.checkpoints.enabled in setup.json)."}, nil
	}
	list, err := c.store.ListFileCheckpoints(ctx, session.ID, 0)
	if err != nil {
		return FileCheckpointsResponse{}, err
	}
	views := make([]FileCheckpointView, 0, len(list))
	if len(list) == 0 {
		return FileCheckpointsResponse{Checkpoints: views}, nil
	}
	// A session's checkpoints all share its working directory unless it changed.
	trees := make([]string, 0, len(list))
	for _, cp := range list {
		if cp.WorkingDir == session.WorkingDir {
			trees = append(trees, cp.Tree)
		}
	}
	changes, err := c.checkpoints.Compare(ctx, session.WorkingDir, trees)
	if errors.Is(err, checkpoint.ErrUnsupported) {
		return FileCheckpointsResponse{Checkpoints: views, Unavailable: err.Error()}, nil
	}
	if err != nil {
		return FileCheckpointsResponse{}, err
	}
	i := 0
	for _, cp := range list {
		if cp.WorkingDir != session.WorkingDir {
			continue
		}
		views = append(views, FileCheckpointView{FileCheckpoint: cp, Changes: changes[i]})
		i++
	}
	return FileCheckpointsResponse{Checkpoints: views}, nil
}

// RestoreFileCheckpoint brings the session's working directory back to the
// checkpoint while no run is active. It first checkpoints the state it
// replaces, so a restore can be undone, and leaves the model a note naming
// the files it changed.
func (c *Core) RestoreFileCheckpoint(ctx context.Context, sessionID, checkpointID string) (FileCheckpointRestoreResponse, error) {
	if c.checkpoints == nil {
		return FileCheckpointRestoreResponse{}, fmt.Errorf("%w: file checkpoints are off", ErrInvalidInput)
	}
	cp, err := c.store.GetFileCheckpoint(ctx, strings.TrimSpace(checkpointID))
	if err != nil {
		return FileCheckpointRestoreResponse{}, err
	}
	session, err := c.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return FileCheckpointRestoreResponse{}, err
	}
	if cp.SessionID != session.ID || cp.WorkingDir != session.WorkingDir {
		return FileCheckpointRestoreResponse{}, ErrNotFound
	}
	if active, err := c.sessionRunActive(ctx, session.ID); err != nil {
		return FileCheckpointRestoreResponse{}, err
	} else if active {
		return FileCheckpointRestoreResponse{}, fmt.Errorf("%w: finish or cancel the current run before restoring files", ErrRunActive)
	}
	if _, err := c.takeFileCheckpoint(ctx, session.ID, "", session.WorkingDir, restoreToolName); err != nil {
		return FileCheckpointRestoreResponse{}, err
	}
	changes, err := c.checkpoints.Restore(ctx, cp.WorkingDir, cp.Tree)
	if errors.Is(err, checkpoint.ErrMissing) {
		return FileCheckpointRestoreResponse{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	if err != nil {
		return FileCheckpointRestoreResponse{}, err
	}
	response := FileCheckpointRestoreResponse{Checkpoint: cp, Changes: changes}
	if len(changes) == 0 {
		return response, nil
	}
	note, err := c.appendRestoreNote(ctx, cp, changes)
	if err != nil {
		return FileCheckpointRestoreResponse{}, err
	}
	response.Note = &note
	return response, nil
}

// maxNotePaths bounds the paths a restore note names.
const maxNotePaths = 30

// appendRestoreNote tells the model which files the user restored, so it
// reads them again instead of trusting what it saw before.
func (c *Core) appendRestoreNote(ctx context.Context, cp FileCheckpoint, changes []checkpoint.Change) (transcript.Message, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "The user restored the files in %s to checkpoint %s, taken %s. Changed:\n", cp.WorkingDir, cp.ID, cp.CreatedAt.Format(time.DateTime+" UTC"))
	for i, change := range changes {
		if i == maxNotePaths {
			fmt.Fprintf(&b, "- and %d more\n", len(changes)-i)
			break
		}
		fmt.Fprintf(&b, "- %s (%s)\n", change.Path, restoredVerb(change.Status))
	}
	b.WriteString("File contents you read before may be stale: read a file again before relying on it or changing it.")
	text := b.String()
	now := c.now().UTC()
	note := transcript.Message{ID: c.newID("msg"), SessionID: cp.SessionID, Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: text, Parts: transcript.NormalizeMessageParts(text, nil), CreatedAt: now, UpdatedAt: now}
	seq, err := c.store.AppendMessage(ctx, note)
	if err != nil {
		return transcript.Message{}, err
	}
	note.Seq = seq
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: cp.SessionID, Payload: note})
	return note, nil
}

// restoredVerb says what a restore did to a file it changed.
func restoredVerb(status checkpoint.Status) string {
	switch status {
	case checkpoint.StatusAdded:
		return "restored"
	case checkpoint.StatusDeleted:
		return "deleted"
	default:
		return "reverted"
	}
}
