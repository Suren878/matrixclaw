package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runStatusEditInterval is the least time between two edits of a run's status
// message; the state a run ends in is written regardless.
const runStatusEditInterval = 2 * time.Second

type runStatusMessage struct {
	messageID int64
	text      string
	editedAt  time.Time
}

// renderRunStatusMessage keeps one silent message per run with its state, steps,
// running tools, background tasks and todo list. It appears once the run calls a
// tool or waits and is edited when its text changes; a flood wait is returned so
// the delivery retries after it, other refusals only skip the edit.
func (w *Worker) renderRunStatusMessage(ctx context.Context, target chatTarget, daemon *daemonclient.Client, run core.Run, messages []transcript.Message, state *runDeliveryState) error {
	status := &state.status
	final := runFinished(run.Status)
	if status.messageID == 0 && (final || !runStatusShown(run, messages)) {
		return nil
	}
	if !final && w.nowUTC().Before(status.editedAt.Add(runStatusEditInterval)) {
		return nil
	}
	progress, err := daemon.RunProgress(ctx, run.ID)
	if retryableDeliveryError(err) {
		return err
	}
	approvals, err := daemon.ListApprovals(ctx, run.SessionID, core.ApprovalStatePending)
	if retryableDeliveryError(err) {
		return err
	}
	// Without progress the message still shows the state, tools and todo.
	text := renderRunStatusText(run, progress, messages, askedCalls(approvals, run.ID))
	if text == status.text {
		return nil
	}
	messageID, err := w.writeRunStatus(silentTelegramDelivery(ctx), target, status.messageID, text)
	if err != nil && (IsRetryable(err) || ctx.Err() != nil) {
		return err
	}
	if err != nil {
		log.Printf("telegram: run status message failed chat=%d run=%s: %v", target.chatID, run.ID, err)
	}
	*status = runStatusMessage{messageID: messageID, text: text, editedAt: w.nowUTC()}
	return nil
}

// writeRunStatus edits the status message, or sends a new one when there is none
// or it was deleted or can no longer be edited.
func (w *Worker) writeRunStatus(ctx context.Context, target chatTarget, messageID int64, text string) (int64, error) {
	formatted := formatTelegramText(text)
	if messageID != 0 {
		err := w.editFormattedMessage(ctx, EditMessageTextRequest{ChatID: target.chatID, MessageID: messageID}, formatted)
		if err == nil || isTelegramMessageNotModified(err) {
			return messageID, nil
		}
		if !shouldFallbackTelegramEdit(err) {
			return messageID, err
		}
	}
	sent, err := w.sendFormattedTelegramMessage(ctx, SendMessageRequest{ChatID: target.chatID, SkipReplyKeyboardRemove: true}, formatted)
	if err != nil {
		return messageID, err
	}
	return sent.MessageID, nil
}

func runFinished(status core.RunStatus) bool {
	return status == core.RunStatusCompleted || status == core.RunStatusFailed || status == core.RunStatusCanceled
}

// runStatusShown is whether a run has more to show than its reply: a plain
// answer gets no status message.
func runStatusShown(run core.Run, messages []transcript.Message) bool {
	if run.Status == core.RunStatusWaitingApproval || run.Status == core.RunStatusWaitingEvents {
		return true
	}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				return true
			}
		}
	}
	return false
}

// askedCalls are the run's calls waiting for approval.
func askedCalls(approvals []core.Approval, runID string) map[string]bool {
	asked := map[string]bool{}
	for _, approval := range approvals {
		if approval.RunID == runID {
			asked[approval.ToolCallRef] = true
		}
	}
	return asked
}

func renderRunStatusText(run core.Run, progress core.RunProgress, messages []transcript.Message, asked map[string]bool) string {
	lines := []string{runStatusHeadline(run) + runStatusSteps(progress)}
	if !runFinished(run.Status) {
		if tools := runningToolsLine(messages, asked); tools != "" {
			lines = append(lines, tools)
		} else if run.Status == core.RunStatusRunning || run.Status == core.RunStatusAccepted {
			lines = append(lines, "Thinking...")
		}
	}
	if progress.Tasks > 0 {
		lines = append(lines, fmt.Sprintf("Background tasks: %d", progress.Tasks))
	}
	if items, ok := runTodo(messages, run.ID); ok && len(items) > 0 {
		lines = append(lines, "", renderTelegramTodo(items))
	}
	return clipTelegramText(strings.Join(lines, "\n"))
}

func runStatusHeadline(run core.Run) string {
	switch run.Status {
	case core.RunStatusWaitingApproval:
		return "✋ Waiting for approval"
	case core.RunStatusWaitingEvents:
		return "⏸ Waiting for background work"
	case core.RunStatusFailed:
		return "❌ Failed"
	case core.RunStatusCanceled:
		return "⛔ Canceled"
	case core.RunStatusCompleted:
		switch run.StopReason {
		case agent.StopBudgetExhausted:
			return "⚠️ Stopped at the budget"
		case agent.StopLoopDetected:
			return "⚠️ Stopped: repeating the same step"
		case agent.StopContextExhausted:
			return "⚠️ Stopped: the context is full"
		default:
			return "✅ Done"
		}
	default:
		return "⏳ Working"
	}
}

func runStatusSteps(progress core.RunProgress) string {
	switch {
	case progress.Steps == 0:
		return ""
	case progress.StepLimit > 0:
		return fmt.Sprintf(" · step %d/%d", progress.Steps, progress.StepLimit)
	default:
		return fmt.Sprintf(" · step %d", progress.Steps)
	}
}

// runningToolsLine names the first tool call still without a result that is
// neither held back nor asked, and how many more run with it.
func runningToolsLine(messages []transcript.Message, asked map[string]bool) string {
	answered := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil {
				answered[part.ToolResult.ToolCallID] = true
			}
		}
	}
	var running []transcript.ToolCallPart
	for _, message := range messages {
		for _, part := range message.Parts {
			if call := part.ToolCall; call != nil && !answered[call.ID] && !call.Deferred && !asked[call.ID] && call.Name != todo.ToolName {
				running = append(running, *call)
			}
		}
	}
	if len(running) == 0 {
		return ""
	}
	action, detail := telegramToolAction(running[0])
	line := action + telegramToolDetailSuffix(detail)
	if len(running) > 1 {
		line += fmt.Sprintf(" (+%d more)", len(running)-1)
	}
	return line
}
