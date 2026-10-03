package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (w *Worker) setPrompt(externalKey string, prompt controlplane.PromptData) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prompts[externalKey] = prompt
}

func (w *Worker) clearPrompt(externalKey string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.prompts, externalKey)
}

func (w *Worker) prompt(externalKey string) (controlplane.PromptData, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	prompt, ok := w.prompts[externalKey]
	return prompt, ok
}

func (w *Worker) handlePendingPrompt(ctx context.Context, target chatTarget, text string) (bool, error) {
	prompt, ok := w.prompt(target.externalKey)
	if !ok {
		return false, nil
	}
	if prompt.Sensitive && target.isChat() {
		_ = w.api.DeleteMessage(ctx, DeleteMessageRequest{
			ChatID:    target.chatID,
			MessageID: target.messageID,
		})
	}
	if isPromptCloseCommand(text) {
		w.clearPrompt(target.externalKey)
		if approvalID, ok := controlplane.DeniedApproval(prompt.SubmitCommandPrefix); ok {
			return true, w.askApprovalAgain(ctx, target, approvalID)
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
		w.clearPrompt(target.externalKey)
		return false, nil
	}
	w.clearPrompt(target.externalKey)
	if w.denialDecidedElsewhere(ctx, target, prompt) {
		return false, nil
	}
	result, err := w.dispatcher(target).Handle(ctx, prompt.SubmitCommandPrefix+strings.TrimSpace(text))
	if err != nil {
		return true, w.sendText(ctx, target, fmt.Sprintf("Command failed: %v", err))
	}
	return true, w.renderCommandResult(ctx, target, result)
}

// denialDecidedElsewhere reports whether prompt asks why to deny an approval of
// the chat's session that is no longer pending, so the text is a new message.
func (w *Worker) denialDecidedElsewhere(ctx context.Context, target chatTarget, prompt controlplane.PromptData) bool {
	approvalID, ok := controlplane.DeniedApproval(prompt.SubmitCommandPrefix)
	if !ok {
		return false
	}
	_, pending, known := w.sessionApproval(ctx, target, approvalID)
	return known && !pending
}

// askApprovalAgain asks for an approval whose denial reason was canceled, with
// its buttons, while it is pending.
func (w *Worker) askApprovalAgain(ctx context.Context, target chatTarget, approvalID string) error {
	approval, pending, _ := w.sessionApproval(ctx, target, approvalID)
	if !pending {
		return w.sendText(ctx, target, "Closed.")
	}
	_, err := w.sendApprovalMessage(ctx, target, approval)
	return err
}

// sessionApproval finds the approval among the pending ones of the chat's
// session; known is false when that cannot be told.
func (w *Worker) sessionApproval(ctx context.Context, target chatTarget, approvalID string) (approval core.Approval, pending bool, known bool) {
	daemon := w.daemon(target.externalKey)
	binding, err := daemon.CurrentBinding(ctx)
	if err != nil || strings.TrimSpace(binding.SessionID) == "" {
		return core.Approval{}, false, false
	}
	approvals, err := daemon.ListApprovals(ctx, binding.SessionID, core.ApprovalStatePending)
	if err != nil {
		return core.Approval{}, false, false
	}
	for _, approval := range approvals {
		if approval.ID == approvalID {
			return approval, true, true
		}
	}
	return core.Approval{}, false, true
}

func isPromptCloseCommand(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return text == "/close" || text == "/cancel"
}
