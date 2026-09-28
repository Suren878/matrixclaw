package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
)

type RunCheckpointPhase string

const (
	RunCheckpointPhaseModel           RunCheckpointPhase = "model"
	RunCheckpointPhaseTool            RunCheckpointPhase = "tool"
	RunCheckpointPhaseExternalAgent   RunCheckpointPhase = "external_agent"
	RunCheckpointPhaseWaitingApproval RunCheckpointPhase = "waiting_approval"
	RunCheckpointPhaseWaitingSubagent RunCheckpointPhase = "waiting_subagent"
	RunCheckpointPhaseRecovering      RunCheckpointPhase = "recovering"
	runRecoveryReasonDaemonRestart                       = "daemon_restart"
)

type RunCheckpoint struct {
	RunID          string             `json:"run_id"`
	Phase          RunCheckpointPhase `json:"phase"`
	ToolCallID     string             `json:"tool_call_id,omitempty"`
	ToolName       string             `json:"tool_name,omitempty"`
	RecoveryCount  int                `json:"recovery_count,omitempty"`
	RecoveryReason string             `json:"recovery_reason,omitempty"`
	EngineState    json.RawMessage    `json:"engine_state,omitempty"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// RunCheckpointStore is optional so lightweight in-memory Store
// implementations remain compatible. The production SQLite store implements
// it and persists execution boundaries used for crash recovery.
type RunCheckpointStore interface {
	SaveRunCheckpoint(ctx context.Context, checkpoint RunCheckpoint) error
	GetRunCheckpoint(ctx context.Context, runID string) (RunCheckpoint, error)
	DeleteRunCheckpoint(ctx context.Context, runID string) error
}

func (c *Core) saveRunCheckpoint(ctx context.Context, runID string, phase RunCheckpointPhase, toolCallID string, toolName string) error {
	return c.updateRunCheckpoint(ctx, runID, func(checkpoint *RunCheckpoint) {
		checkpoint.Phase = phase
		checkpoint.ToolCallID = normalizeText(toolCallID)
		checkpoint.ToolName = normalizeText(toolName)
	})
}

// saveEngineCheckpoint stores the engine's phase together with its run counters.
func (c *Core) saveEngineCheckpoint(ctx context.Context, state agent.State) error {
	counters, err := json.Marshal(state.Counters)
	if err != nil {
		return err
	}
	return c.updateRunCheckpoint(ctx, state.RunID, func(checkpoint *RunCheckpoint) {
		checkpoint.Phase = RunCheckpointPhase(state.Phase)
		checkpoint.ToolCallID = normalizeText(state.ToolCallID)
		checkpoint.ToolName = normalizeText(state.ToolName)
		checkpoint.EngineState = counters
	})
}

// updateRunCheckpoint applies update to the run's checkpoint and keeps every field
// update leaves alone, such as the recovery count or the engine counters.
func (c *Core) updateRunCheckpoint(ctx context.Context, runID string, update func(*RunCheckpoint)) error {
	store, ok := c.store.(RunCheckpointStore)
	if !ok {
		return nil
	}
	runID = normalizeText(runID)
	if runID == "" {
		return nil
	}
	checkpoint, err := store.GetRunCheckpoint(ctx, runID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	checkpoint.RunID = runID
	update(&checkpoint)
	checkpoint.UpdatedAt = c.now().UTC()
	return store.SaveRunCheckpoint(ctx, checkpoint)
}

// resumeCounters reads the engine counters of the run's checkpoint; a checkpoint
// without readable counters starts the budget from zero.
func (c *Core) resumeCounters(ctx context.Context, runID string) (agent.Counters, error) {
	checkpoint, ok, err := c.runCheckpoint(ctx, runID)
	if err != nil || !ok || len(checkpoint.EngineState) == 0 {
		return agent.Counters{}, err
	}
	var counters agent.Counters
	if err := json.Unmarshal(checkpoint.EngineState, &counters); err != nil {
		log.Printf("core: run %q has unreadable budget counters, starting from zero: %v", runID, err)
		return agent.Counters{}, nil
	}
	return counters, nil
}

// carriedCounters are the counters the session's previous run left for the
// next one; unreadable ones start from zero.
func (c *Core) carriedCounters(ctx context.Context, sessionID string) (agent.Counters, error) {
	state, err := c.store.GetSessionEngineState(ctx, sessionID)
	if err != nil || len(state) == 0 {
		return agent.Counters{}, err
	}
	var counters agent.Counters
	if err := json.Unmarshal(state, &counters); err != nil {
		log.Printf("core: session %q has unreadable carried counters, starting from zero: %v", sessionID, err)
		return agent.Counters{}, nil
	}
	return counters, nil
}

// carryCounters keeps what a run's counters carry over to the session's next run.
func (c *Core) carryCounters(ctx context.Context, sessionID string, counters agent.Counters) {
	carried := counters.Carried()
	if carried == (agent.Counters{}) {
		return
	}
	state, err := json.Marshal(carried)
	if err == nil {
		err = c.store.SaveSessionEngineState(ctx, sessionID, state, c.now().UTC())
	}
	if err != nil {
		log.Printf("core: keep carried counters of session %q: %v", sessionID, err)
	}
}

func (c *Core) markRunRecovery(ctx context.Context, runID string) (RunCheckpoint, error) {
	store, ok := c.store.(RunCheckpointStore)
	if !ok {
		return RunCheckpoint{RunID: normalizeText(runID), Phase: RunCheckpointPhaseRecovering, RecoveryCount: 1, RecoveryReason: runRecoveryReasonDaemonRestart, UpdatedAt: c.now().UTC()}, nil
	}
	runID = normalizeText(runID)
	checkpoint, err := store.GetRunCheckpoint(ctx, runID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return RunCheckpoint{}, err
	}
	checkpoint.RunID = runID
	checkpoint.Phase = RunCheckpointPhaseRecovering
	checkpoint.ToolCallID = ""
	checkpoint.ToolName = ""
	checkpoint.RecoveryCount++
	checkpoint.RecoveryReason = runRecoveryReasonDaemonRestart
	checkpoint.UpdatedAt = c.now().UTC()
	if err := store.SaveRunCheckpoint(ctx, checkpoint); err != nil {
		return RunCheckpoint{}, err
	}
	return checkpoint, nil
}

func (c *Core) runCheckpoint(ctx context.Context, runID string) (RunCheckpoint, bool, error) {
	store, ok := c.store.(RunCheckpointStore)
	if !ok {
		return RunCheckpoint{}, false, nil
	}
	checkpoint, err := store.GetRunCheckpoint(ctx, normalizeText(runID))
	if errors.Is(err, ErrNotFound) {
		return RunCheckpoint{}, false, nil
	}
	if err != nil {
		return RunCheckpoint{}, false, err
	}
	return checkpoint, true, nil
}

func (c *Core) clearRunCheckpoint(ctx context.Context, runID string) {
	store, ok := c.store.(RunCheckpointStore)
	if !ok || strings.TrimSpace(runID) == "" {
		return
	}
	_ = store.DeleteRunCheckpoint(ctx, normalizeText(runID))
}

func runCheckpointRecoveryPrompt(checkpoint RunCheckpoint) string {
	if checkpoint.RecoveryCount <= 0 || checkpoint.RecoveryReason != runRecoveryReasonDaemonRestart {
		return ""
	}
	return "Recovery notice: the MatrixClaw daemon restarted while this run was active. Continue from the durable conversation and workspace state. Do not repeat a mutating action merely because its previous result is missing. Inspect the current state first, and only retry a mutation when an explicit recovery approval or clear evidence shows it is still required."
}
