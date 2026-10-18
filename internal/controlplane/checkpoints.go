package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// maxStatLines bounds the files a confirmation lists.
const maxStatLines = 15

func undoCommand(parts ...string) string {
	return controlplaneCommand(append([]string{"undo"}, parts...)...)
}

func rewindCommand(parts ...string) string {
	return controlplaneCommand(append([]string{"rewind"}, parts...)...)
}

// handleUndo restores the files to their state before the last run that
// changed them, after a confirmation showing what changes.
func (d *Dispatcher) handleUndo(ctx context.Context, args string) (Result, error) {
	sessionID, list, refusal, err := d.checkpoints(ctx)
	if err != nil || refusal != "" {
		return textResult(refusal), err
	}
	if id, ok := strings.CutPrefix(strings.TrimSpace(args), "confirm "); ok {
		return d.restoreCheckpoint(ctx, sessionID, strings.TrimSpace(id))
	}
	target, ok := undoTarget(list.Checkpoints)
	if !ok {
		return textResult("Nothing to undo: no run in this session changed files."), nil
	}
	if len(target.Changes) == 0 {
		return textResult("The files already match their state before the last run."), nil
	}
	return Result{Screen: ConfirmData{
		Title:          "Undo",
		Message:        "Restore the files to their state before the last run (" + checkpointTime(target) + ")?\n\n" + statPreview(target),
		ConfirmLabel:   "Restore",
		ConfirmCommand: undoCommand("confirm", target.ID),
		ConfirmDanger:  true,
	}}, nil
}

// handleRewind lists the session's checkpoints, confirms one with what
// restoring it changes, and restores it.
func (d *Dispatcher) handleRewind(ctx context.Context, args string) (Result, error) {
	sessionID, list, refusal, err := d.checkpoints(ctx)
	if err != nil || refusal != "" {
		return textResult(refusal), err
	}
	args = strings.TrimSpace(args)
	if id, ok := strings.CutPrefix(args, "confirm "); ok {
		return d.restoreCheckpoint(ctx, sessionID, strings.TrimSpace(id))
	}
	if args != "" {
		for _, cp := range list.Checkpoints {
			if cp.ID != args {
				continue
			}
			if len(cp.Changes) == 0 {
				return textResult("The files already match this checkpoint."), nil
			}
			return Result{Screen: ConfirmData{
				Title:          "Rewind",
				Message:        "Restore the files to checkpoint " + checkpointLabel(cp) + "?\n\n" + statPreview(cp),
				ConfirmLabel:   "Restore",
				CancelCommand:  rewindCommand(),
				ConfirmCommand: rewindCommand("confirm", cp.ID),
				ConfirmDanger:  true,
			}}, nil
		}
		return textResult("No such checkpoint in this session."), nil
	}
	if len(list.Checkpoints) == 0 {
		return textResult("No checkpoints yet: one is taken before each tool batch that changes files."), nil
	}
	items := make([]PickerItem, 0, len(list.Checkpoints))
	for _, cp := range list.Checkpoints {
		items = append(items, PickerItem{ID: cp.ID, Title: checkpointLabel(cp), Info: changesSummary(cp), Command: rewindCommand(cp.ID)})
	}
	return Result{Screen: NewPickerData(PickerCheckpoints, "Rewind files").
		Meta("Restoring a checkpoint changes the files listed with it.").
		Command(rewindCommand()).Items(items...).Build()}, nil
}

// checkpoints loads the current session's checkpoints; refusal is the text
// to show instead when there is nothing this client may restore.
func (d *Dispatcher) checkpoints(ctx context.Context) (string, core.FileCheckpointsResponse, string, error) {
	if !d.owner() {
		return "", core.FileCheckpointsResponse{}, "Only the owner can restore files.", nil
	}
	sessionID, _, err := d.currentSession(ctx)
	if err != nil {
		return "", core.FileCheckpointsResponse{}, "", err
	}
	if sessionID == "" {
		return "", core.FileCheckpointsResponse{}, "Select or create a session first.", nil
	}
	list, err := d.daemon.FileCheckpoints(ctx, sessionID)
	if err != nil {
		return "", core.FileCheckpointsResponse{}, "", err
	}
	if list.Unavailable != "" {
		return "", core.FileCheckpointsResponse{}, list.Unavailable, nil
	}
	return sessionID, list, "", nil
}

func (d *Dispatcher) restoreCheckpoint(ctx context.Context, sessionID, id string) (Result, error) {
	restored, err := d.daemon.RestoreFileCheckpoint(ctx, sessionID, id)
	if err != nil {
		return Result{}, err
	}
	if len(restored.Changes) == 0 {
		return textResult("The files already matched the checkpoint."), nil
	}
	return Result{Screen: TextData{Text: fmt.Sprintf("Restored %d files to checkpoint %s. /rewind can undo this.", len(restored.Changes), checkpointLabel(core.FileCheckpointView{FileCheckpoint: restored.Checkpoint}))}, ReloadSnapshot: true}, nil
}

// undoTarget is the first checkpoint of the newest run that took one.
func undoTarget(list []core.FileCheckpointView) (core.FileCheckpointView, bool) {
	runID := ""
	var target core.FileCheckpointView
	for _, cp := range list {
		if cp.RunID == "" || (runID != "" && cp.RunID != runID) {
			continue
		}
		runID, target = cp.RunID, cp
	}
	return target, runID != ""
}

func checkpointTime(cp core.FileCheckpointView) string {
	return cp.CreatedAt.Local().Format("Jan 2 15:04:05")
}

func checkpointLabel(cp core.FileCheckpointView) string {
	if cp.RunID == "" {
		return checkpointTime(cp) + ", before a restore"
	}
	return checkpointTime(cp) + ", before " + cp.ToolName
}

func changesSummary(cp core.FileCheckpointView) string {
	if len(cp.Changes) == 0 {
		return "no changes"
	}
	added, deleted := 0, 0
	for _, change := range cp.Changes {
		added += change.Added
		deleted += change.Deleted
	}
	return fmt.Sprintf("%d files, +%d -%d", len(cp.Changes), added, deleted)
}

// statPreview lists what restoring cp changes, at most maxStatLines files.
func statPreview(cp core.FileCheckpointView) string {
	stat := strings.Split(core.DiffStat(cp.Changes), "\n")
	if files := stat[:len(stat)-1]; len(files) > maxStatLines {
		stat = append(append(files[:maxStatLines:maxStatLines], fmt.Sprintf("... %d more", len(files)-maxStatLines)), stat[len(stat)-1])
	}
	return strings.Join(stat, "\n")
}
