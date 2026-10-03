package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

const runScheduleLease = 30 * time.Second

func (c *Core) AcceptRun(ctx context.Context, input HandleMessageInput) (AcceptRunResult, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Client = strings.TrimSpace(input.Client)
	input.ExternalKey = strings.TrimSpace(input.ExternalKey)
	input.WorkingDir = strings.TrimSpace(input.WorkingDir)
	if input.Continue {
		return c.acceptContinueRun(ctx, input)
	}
	text := strings.TrimSpace(input.Text)
	parts := transcript.NormalizeMessageParts(text, input.Parts)
	if text == "" && !messagePartsHaveUserContent(parts) {
		return AcceptRunResult{}, fmt.Errorf("%w: message text is required", ErrInvalidInput)
	}

	session, err := c.resolveSession(ctx, input)
	if err != nil {
		return AcceptRunResult{}, err
	}

	var result AcceptRunResult
	var startRunID string
	var interruptRunID string
	var wakeRunID string
	gate := c.sessionGate(session.ID)
	gate.Lock()
	active, err := c.store.GetActiveRunBySession(ctx, session.ID)
	switch {
	case err == nil:
		pending, createErr := c.createPendingSessionInput(ctx, session, active, input, text, parts)
		if createErr == nil && pending.Mode == BusyInputModeSteer {
			createErr = c.deliverRunToSteerer(ctx, active, input, text, parts)
		}
		if createErr != nil {
			gate.Unlock()
			return AcceptRunResult{}, createErr
		}
		result = AcceptRunResult{
			SessionID: session.ID,
			Status:    acceptRunStatusForInputMode(pending.Mode),
			Input:     &pending,
		}
		switch {
		case pending.Mode == BusyInputModeInterrupt:
			interruptRunID = active.ID
		case pending.Mode == BusyInputModeSteer && active.Status == RunStatusWaitingEvents:
			wakeRunID = active.ID
		}
	case errors.Is(err, ErrNotFound):
		result, err = c.createAcceptedRun(ctx, session, clientRun(text, parts, input.Client, input.ExternalKey, input.ClientCapabilities, deliveryTo{input.DeliveryAddress, input.ReplyOnce}))
		if err != nil {
			gate.Unlock()
			return AcceptRunResult{}, err
		}
		startRunID = result.Run.ID
	default:
		gate.Unlock()
		return AcceptRunResult{}, err
	}
	gate.Unlock()

	if wakeRunID != "" {
		if err := c.wakeWaitingRun(ctx, session.ID, wakeRunID); err != nil {
			return result, err
		}
	}
	if interruptRunID != "" {
		if _, err := c.CancelRun(ctx, interruptRunID); err != nil {
			return result, err
		}
		if _, err := c.startNextPendingSessionInput(ctx, session.ID); err != nil {
			return result, err
		}
	}

	// The daemon hands execution off here; transport is already out of the picture.
	if startRunID != "" {
		if err := c.startRun(ctx, startRunID); err != nil {
			return c.failAcceptedRun(ctx, result, err)
		}
	}

	return result, nil
}

// deliverRunToSteerer delivers the run a message steers to the client it came
// from as well, unless the run is delivered there already.
func (c *Core) deliverRunToSteerer(ctx context.Context, run Run, input HandleMessageInput, text string, parts []transcript.MessagePart) error {
	if !input.ClientCapabilities.ReceivesDeliveries {
		return nil
	}
	delivery, ok, err := c.prepareSessionRunDelivery(run, text, parts, input.Client, input.ExternalKey, deliveryTo{input.DeliveryAddress, input.ReplyOnce})
	if err != nil || !ok {
		return err
	}
	existing, err := c.store.ListClientDeliveries(ctx, ClientDeliveryFilter{Client: delivery.Client, ExternalKey: delivery.ExternalKey, RunID: run.ID, Type: ClientDeliveryTypeRun, Limit: 1})
	if err != nil || len(existing) > 0 {
		return err
	}
	return c.store.CreateClientDelivery(ctx, delivery)
}

func messagePartsHaveUserContent(parts []transcript.MessagePart) bool {
	for _, part := range parts {
		if part.Text != nil && strings.TrimSpace(part.Text.Text) != "" {
			return true
		}
		if part.Image != nil && (strings.TrimSpace(part.Image.DataBase64) != "" || strings.TrimSpace(part.Image.StoragePath) != "") {
			return true
		}
	}
	return false
}

func (c *Core) AcceptTriggeredRun(ctx context.Context, input HandleTriggeredRunInput) (AcceptRunResult, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return AcceptRunResult{}, fmt.Errorf("%w: message text is required", ErrInvalidInput)
	}
	triggerID := strings.TrimSpace(input.TriggerID)
	if triggerID == "" {
		return AcceptRunResult{}, fmt.Errorf("%w: trigger id is required", ErrInvalidInput)
	}
	session, err := c.resolveSession(ctx, HandleMessageInput{
		SessionID:  strings.TrimSpace(input.SessionID),
		WorkingDir: input.WorkingDir,
	})
	if err != nil {
		return AcceptRunResult{}, err
	}
	runID := deterministicRunID(triggerID)
	if result, ok, err := c.triggeredRun(ctx, runID, text); err != nil || ok {
		return result, err
	}

	// A session busy with another run refuses the trigger; automation tries it again later.
	gate := c.sessionGate(session.ID)
	gate.Lock()
	if _, err := c.store.GetActiveRunBySession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		gate.Unlock()
		if err == nil {
			err = fmt.Errorf("%w: the session is busy with another run", ErrRunActive)
		}
		return AcceptRunResult{}, err
	}
	result, err := c.createAcceptedRun(ctx, session, newRun{
		RunID:        runID,
		MessageID:    deterministicMessageID(triggerID),
		Text:         text,
		Parts:        transcript.NormalizeMessageParts(text, nil),
		Client:       strings.TrimSpace(input.Client),
		ExternalKey:  strings.TrimSpace(input.ExternalKey),
		Capabilities: input.ClientCapabilities,
		Trigger:      RunTriggerAutomation,
	})
	gate.Unlock()
	if err != nil {
		if existing, ok, _ := c.triggeredRun(ctx, runID, text); ok {
			return existing, nil
		}
		return AcceptRunResult{}, err
	}
	if err := c.startRun(ctx, result.Run.ID); err != nil {
		return c.failAcceptedRun(ctx, result, err)
	}
	return result, nil
}

func (c *Core) startRun(ctx context.Context, runID string) error {
	if runID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	now := c.now().UTC()
	c.mu.Lock()
	if c.activeRuns[runID] != nil {
		c.mu.Unlock()
		return nil
	}
	if scheduledAt, ok := c.scheduledRuns[runID]; ok {
		age := now.Sub(scheduledAt)
		if age >= 0 && age < runScheduleLease {
			c.mu.Unlock()
			return nil
		}
	}
	if c.scheduledRuns == nil {
		c.scheduledRuns = map[string]time.Time{}
	}
	c.scheduledRuns[runID] = now
	c.mu.Unlock()
	if err := c.runStarter.StartRun(ctx, runID); err != nil {
		c.mu.Lock()
		delete(c.scheduledRuns, runID)
		c.mu.Unlock()
		return err
	}
	return nil
}

// triggeredRun is the run a trigger already started, if any.
func (c *Core) triggeredRun(ctx context.Context, runID string, text string) (AcceptRunResult, bool, error) {
	existing, err := c.store.GetRun(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, false, nil
	}
	if err != nil {
		return AcceptRunResult{}, false, err
	}
	message := transcript.Message{ID: existing.UserMessageID, SessionID: existing.SessionID, RunID: existing.ID, Role: transcript.MessageRoleUser, Content: text}
	if stored, getErr := c.store.GetMessage(ctx, existing.UserMessageID); getErr == nil {
		message = stored
	}
	return AcceptRunResult{SessionID: existing.SessionID, Status: AcceptRunStatusStarted, UserMessage: message, Run: existing}, true, nil
}

func deterministicRunID(triggerID string) string {
	return "run_" + stableIDPart(triggerID)
}

func deterministicMessageID(triggerID string) string {
	return "msg_" + stableIDPart(triggerID)
}

func stableIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "automation"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func (c *Core) GetRun(ctx context.Context, runID string) (Run, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return Run{}, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	return c.store.GetRun(ctx, runID)
}

func (c *Core) CancelRun(ctx context.Context, runID string) (Run, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return Run{}, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}

	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	if run.Status.Terminal() {
		return run, nil
	}
	stopped, err := c.cancelRunRecords(ctx, &run)
	if errors.Is(err, ErrRunEnded) {
		// The run ended on its own meanwhile; that end stands.
		return c.store.GetRun(ctx, run.ID)
	}
	if err != nil {
		return Run{}, err
	}
	for _, id := range stopped {
		c.cancelActiveRun(id)
	}
	if !c.runIsActive(run.ID) {
		// No execution of the run is left to do what follows its end; one that
		// released just now did it too, and a repeat is harmless.
		c.afterRun(ctx, run.ID)
	}
	return run, nil
}

// cancelRunRecords marks the run and its active subagent children canceled before
// any of them is stopped, so a stopped child is not kept for recovery, then stops
// the commands they run in the background. It returns the ids of the runs to stop.
func (c *Core) cancelRunRecords(ctx context.Context, run *Run) ([]string, error) {
	children, err := c.cancelSubagentChildren(ctx, *run, false)
	if err != nil {
		return nil, err
	}
	if err := c.transition(ctx, run, runChange{To: RunStatusCanceled, Err: "canceled by user"}); err != nil {
		return nil, err
	}
	if err := c.stopRunCommands(ctx, *run); err != nil {
		return nil, err
	}
	return append([]string{run.ID}, children...), nil
}

// cancelSubagentChildren cancels the active subagent tasks the run started,
// only its blocking ones with blockingOnly, and returns the ids of their runs
// to stop. Each task ends delivered first, so its end is no event.
func (c *Core) cancelSubagentChildren(ctx context.Context, run Run, blockingOnly bool) ([]string, error) {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: run.SessionID, RunID: run.ID, Kind: TaskKindSubagent, Statuses: activeTaskStatuses()})
	if err != nil {
		return nil, err
	}
	var stopped []string
	for _, task := range tasks {
		if blockingOnly && task.Background {
			continue
		}
		const summary = "Subagent canceled with its parent run."
		if _, _, err := c.endSubagentTask(ctx, task, TaskEnd{Status: TaskStatusCanceled, Summary: summary, Error: summary, Delivered: true}); err != nil {
			return nil, err
		}
		child, err := c.store.GetRun(ctx, task.ChildRunID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if err == nil && !child.Status.Terminal() {
			ids, err := c.cancelRunRecords(ctx, &child)
			if err != nil && !errors.Is(err, ErrRunEnded) {
				return nil, err
			}
			stopped = append(stopped, ids...)
		}
	}
	return stopped, nil
}

// rejectRunApprovals rejects the approvals an ended run leaves pending and
// tells clients the calls that asked for them failed with errText.
func (c *Core) rejectRunApprovals(ctx context.Context, run Run, errText string) error {
	approvals, err := c.store.ListRunApprovals(ctx, run.SessionID, run.ID)
	if err != nil {
		return err
	}
	for _, approval := range approvals {
		if approval.State != ApprovalStatePending {
			continue
		}
		approval.State = ApprovalStateRejected
		decidedAt := c.now().UTC()
		approval.DecidedAt = &decidedAt
		err := c.store.DecideApproval(ctx, approval)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		audience, audienceRun := c.approvalAudience(ctx, approval)
		c.publishEvent(Event{
			Type:      EventApprovalResult,
			SessionID: audience,
			RunID:     audienceRun,
			Payload:   PermissionNotification{ApprovalID: approval.ID, ToolCallID: approval.ToolCallRef, Denied: true},
		})
		c.publishToolUpdate(approval.SessionID, approval.RunID, ToolUpdate{
			ToolCallID: approval.ToolCallRef,
			ToolName:   approval.ToolName,
			State:      ToolLifecycleFailed,
			RunID:      approval.RunID,
			SessionID:  approval.SessionID,
			ApprovalID: approval.ID,
			Error:      errText,
		})
	}
	return nil
}
