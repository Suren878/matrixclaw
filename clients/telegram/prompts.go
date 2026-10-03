package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

// promptLifetime is how long a prompt takes the chat's next message as its
// answer; a later message is an ordinary one.
const promptLifetime = 10 * time.Minute

type pendingPrompt struct {
	controlplane.PromptData
	askedAt time.Time
	// denial is the approval a denial reason prompt is for.
	denial approvalRef
}

// approvalRef is an approval and the chat message that asks for it.
type approvalRef struct {
	id        string
	sessionID string
	messageID int64
}

// replacePrompt makes next (nil for none) the chat's prompt; an approval whose
// denial reason prompt it replaces is asked for again.
func (w *Worker) replacePrompt(ctx context.Context, target chatTarget, next *pendingPrompt) {
	w.mu.Lock()
	previous, had := w.prompts[target.externalKey]
	if next == nil {
		delete(w.prompts, target.externalKey)
	} else {
		next.askedAt = w.nowUTC()
		w.prompts[target.externalKey] = *next
	}
	w.mu.Unlock()
	if had && (next == nil || next.denial.id != previous.denial.id) {
		if err := w.restoreApproval(ctx, target, previous, false); err != nil {
			log.Printf("telegram: restore approval %s failed: %v", previous.denial.id, err)
		}
	}
}

// takePrompt removes and returns the chat's prompt.
func (w *Worker) takePrompt(externalKey string) (pendingPrompt, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	prompt, ok := w.prompts[externalKey]
	delete(w.prompts, externalKey)
	return prompt, ok
}

// forgetDenial drops the chat's denial reason prompt for approvalID, which was
// decided with a button.
func (w *Worker) forgetDenial(externalKey string, approvalID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.prompts[externalKey].denial.id == approvalID {
		delete(w.prompts, externalKey)
	}
}

func (w *Worker) handlePendingPrompt(ctx context.Context, target chatTarget, text string) (bool, error) {
	prompt, ok := w.takePrompt(target.externalKey)
	if !ok {
		return false, nil
	}
	// A late /cancel still closes the prompt rather than the running task.
	if w.nowUTC().Sub(prompt.askedAt) > promptLifetime && !isPromptCloseCommand(text) {
		if err := w.restoreApproval(ctx, target, prompt, false); err != nil {
			log.Printf("telegram: restore approval %s failed: %v", prompt.denial.id, err)
		}
		return false, nil
	}
	if prompt.Sensitive && target.isChat() {
		_ = w.api.DeleteMessage(ctx, DeleteMessageRequest{
			ChatID:    target.chatID,
			MessageID: target.messageID,
		})
	}
	if isPromptCloseCommand(text) {
		if prompt.denial.id != "" {
			return true, w.restoreApproval(ctx, target, prompt, true)
		}
		if strings.TrimSpace(prompt.CancelCommand) != "" {
			result, err := w.dispatcher(target).Handle(ctx, strings.TrimSpace(prompt.CancelCommand))
			if err != nil {
				return true, w.sendText(ctx, target, fmt.Sprintf("Command failed: %v", err))
			}
			return true, w.renderCommandResult(ctx, target, result)
		}
		return true, w.sendText(ctx, target, "Closed.")
	}
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		if err := w.restoreApproval(ctx, target, prompt, false); err != nil {
			log.Printf("telegram: restore approval %s failed: %v", prompt.denial.id, err)
		}
		return false, nil
	}
	if prompt.denial.id != "" {
		if _, pending, known := w.pendingApproval(ctx, target, prompt.denial); known && !pending {
			// Decided elsewhere meanwhile: the text is a new message.
			return false, nil
		}
	}
	result, err := w.dispatcher(target).Handle(ctx, prompt.SubmitCommandPrefix+strings.TrimSpace(text))
	if err != nil {
		return true, w.sendText(ctx, target, fmt.Sprintf("Command failed: %v", err))
	}
	return true, w.renderCommandResult(ctx, target, result)
}

// restoreApproval gives the approval of a dropped denial reason prompt its
// buttons back while it is pending; announce says "Closed." otherwise.
func (w *Worker) restoreApproval(ctx context.Context, target chatTarget, prompt pendingPrompt, announce bool) error {
	approval, pending := core.Approval{}, false
	if prompt.denial.id != "" {
		approval, pending, _ = w.pendingApproval(ctx, target, prompt.denial)
	}
	if !pending {
		if announce {
			return w.sendText(ctx, target, "Closed.")
		}
		return nil
	}
	return w.editOrSend(ctx, target, prompt.denial.messageID, approvalRequestText(approval), w.approvalKeyboard(target, approval))
}

// pendingApproval finds the approval among the pending ones of its session;
// known is false when that cannot be told.
func (w *Worker) pendingApproval(ctx context.Context, target chatTarget, ref approvalRef) (approval core.Approval, pending bool, known bool) {
	if ref.sessionID == "" {
		return core.Approval{}, false, false
	}
	approvals, err := w.daemon(target.externalKey).ListApprovals(ctx, ref.sessionID, core.ApprovalStatePending)
	if err != nil {
		return core.Approval{}, false, false
	}
	for _, approval := range approvals {
		if approval.ID == ref.id {
			return approval, true, true
		}
	}
	return core.Approval{}, false, true
}

func isPromptCloseCommand(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return text == "/close" || text == "/cancel"
}
