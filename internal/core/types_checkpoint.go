package core

import (
	"context"
	"time"

	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// FileCheckpoint is a snapshot of a session's working directory taken before
// a tool batch changed files (RunID set) or before a restore (RunID empty).
// Seq is the session's newest message when it was taken.
type FileCheckpoint struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	RunID      string    `json:"run_id,omitempty"`
	Seq        int64     `json:"seq"`
	ToolName   string    `json:"tool_name,omitempty"`
	WorkingDir string    `json:"working_dir"`
	Tree       string    `json:"tree"`
	CreatedAt  time.Time `json:"created_at"`
}

// FileCheckpointStore keeps file checkpoints; lists are newest first.
type FileCheckpointStore interface {
	CreateFileCheckpoint(ctx context.Context, checkpoint FileCheckpoint) error
	GetFileCheckpoint(ctx context.Context, id string) (FileCheckpoint, error)
	// ListFileCheckpoints lists the session's checkpoints; limit 0 lists all.
	ListFileCheckpoints(ctx context.Context, sessionID string, limit int) ([]FileCheckpoint, error)
	// TrimFileCheckpoints keeps the newest keep checkpoints of the working
	// directory and reports whether it deleted any.
	TrimFileCheckpoints(ctx context.Context, workingDir string, keep int) (bool, error)
	// FileCheckpointTrees lists the trees the working directory's checkpoints keep.
	FileCheckpointTrees(ctx context.Context, workingDir string) ([]string, error)
}

// FileCheckpointView is a checkpoint with what restoring it would change now.
type FileCheckpointView struct {
	FileCheckpoint
	Changes []checkpoint.Change `json:"changes"`
}

// FileCheckpointsResponse lists a session's checkpoints, newest first;
// Unavailable says why there are none to restore when checkpoints are off or
// the working directory is not snapshotted.
type FileCheckpointsResponse struct {
	Checkpoints []FileCheckpointView `json:"checkpoints"`
	Unavailable string               `json:"unavailable,omitempty"`
}

// FileCheckpointRestoreResponse is what a restore changed and the note it
// left for the model; Note is empty when nothing had to change.
type FileCheckpointRestoreResponse struct {
	Checkpoint FileCheckpoint      `json:"checkpoint"`
	Changes    []checkpoint.Change `json:"changes"`
	Note       *transcript.Message `json:"note,omitempty"`
}
