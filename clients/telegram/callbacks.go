package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func (w *Worker) handleCallbackQuery(ctx context.Context, cq *CallbackQuery) error {
	if cq == nil {
		return nil
	}
	telegramCtx, cancel := context.WithTimeout(ctx, telegramHTTPTimeout)
	defer cancel()
	if strings.HasPrefix(strings.TrimSpace(cq.Data), inlineCallbackPrefix) {
		return w.handleInlineCallback(telegramCtx, cq)
	}
	if cq.Message == nil || !w.allowCallback(cq) {
		return nil
	}
	target := targetFromMessage(cq.Message)

	if resolved := w.resolveCallbackData(cq.Data); resolved != cq.Data {
		copy := *cq
		copy.Data = resolved
		cq = &copy
	}
	if strings.HasPrefix(strings.TrimSpace(cq.Data), cbCallbackRef) {
		return w.api.AnswerCallbackQuery(telegramCtx, AnswerCallbackQueryRequest{
			CallbackQueryID: cq.ID,
			Text:            "Menu expired. Send the command again.",
		})
	}
	_ = w.api.AnswerCallbackQuery(telegramCtx, AnswerCallbackQueryRequest{CallbackQueryID: cq.ID})

	switch {
	case strings.HasPrefix(cq.Data, cbPicker):
		// A picked command such as compaction may outlast the Telegram timeout.
		return w.handlePickerCallback(ctx, target, cq)
	case strings.HasPrefix(cq.Data, cbPickerPage):
		return w.handlePickerPageCallback(telegramCtx, target, cq)
	case strings.HasPrefix(cq.Data, cbApprovalOnce):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalOnce), core.ApprovalResolveRequest{Approved: true})
	case strings.HasPrefix(cq.Data, cbApprovalSession):
		if !w.keepsRules(target, permission.ScopeSession) {
			return w.sendText(telegramCtx, target, "Guests cannot keep permission rules.")
		}
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalSession), core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession})
	case strings.HasPrefix(cq.Data, cbApprovalGlobal):
		if !w.keepsRules(target, permission.ScopeGlobal) {
			return w.sendText(telegramCtx, target, "Only the owner can keep a rule for every session.")
		}
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalGlobal), core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeGlobal})
	case strings.HasPrefix(cq.Data, cbApprovalDeny):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalDeny), core.ApprovalResolveRequest{})
	case strings.HasPrefix(cq.Data, cbApprovalReason):
		return w.askDenialReason(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalReason))
	default:
		return nil
	}
}

func (w *Worker) handlePickerCallback(ctx context.Context, target chatTarget, cq *CallbackQuery) error {
	kind, command, ok := parsePickerCallbackData(cq.Data)
	if !ok {
		return nil
	}
	switch kind {
	case callbackKindCommand:
		if strings.TrimSpace(command) == "" {
			return nil
		}
		return w.dispatchPickerCommand(ctx, target, cq.Message.MessageID, command)
	case callbackKindDismiss:
		return w.deleteMenuMessage(ctx, target, cq.Message.MessageID, cq.Message.Text)
	}
	return nil
}

func (w *Worker) handlePickerPageCallback(ctx context.Context, target chatTarget, cq *CallbackQuery) error {
	command, page, ok := parsePickerPageCallbackData(cq.Data)
	if !ok || strings.TrimSpace(command) == "" {
		return nil
	}
	return w.dispatchCommandAndEditPage(ctx, target, cq.Message.MessageID, command, page)
}

func (w *Worker) dispatchPickerCommand(ctx context.Context, target chatTarget, messageID int64, command string) error {
	if isDaemonRestartCommand(command) {
		return w.dispatchRestartCommandAndEdit(target, messageID)
	}
	if isContextCompactCommand(command) {
		if err := w.editOrSend(ctx, target, messageID, compactProgressText, nil); err != nil {
			return err
		}
	}
	return w.dispatchCommandAndEdit(ctx, target, messageID, command)
}

func (w *Worker) deleteMenuMessage(ctx context.Context, target chatTarget, messageID int64, fallbackText string) error {
	if messageID <= 0 {
		return nil
	}
	if err := w.api.DeleteMessage(ctx, DeleteMessageRequest{ChatID: target.chatID, MessageID: messageID}); err == nil {
		return nil
	}
	return w.editOrSend(ctx, target, messageID, fallbackText, nil)
}

const compactProgressText = "🧠 Compact started..."

func isContextCompactCommand(command string) bool {
	return matchesCatalogCommand(command, controlplane.CommandContext, "compact confirm")
}

// askDenialReason makes the chat's next message the reason for denying the
// approval; its message loses the buttons, which /cancel brings back.
func (w *Worker) askDenialReason(ctx context.Context, target chatTarget, cq *CallbackQuery, approvalID string) error {
	w.setPrompt(target.externalKey, controlplane.DenyWithReasonPrompt(approvalID))
	return w.editOrSend(ctx, target, cq.Message.MessageID, "Denying: send the reason, or /cancel.\n\n"+cq.Message.Text, nil)
}

func (w *Worker) resolveApprovalCallback(ctx context.Context, target chatTarget, cq *CallbackQuery, approvalID string, request core.ApprovalResolveRequest) error {
	if prompt, ok := w.prompt(target.externalKey); ok && prompt.SubmitCommandPrefix == controlplane.DenyWithReasonPrompt(approvalID).SubmitCommandPrefix {
		w.clearPrompt(target.externalKey)
	}
	approval, err := w.daemonFor(target, target.externalKey).ResolveApproval(ctx, approvalID, request)
	if err != nil {
		return w.editOrSend(ctx, target, cq.Message.MessageID, fmt.Sprintf("Resolve approval failed: %v", err), nil)
	}
	return w.editOrSend(ctx, target, cq.Message.MessageID, approvalStatus(approval, request)+"\n\n"+renderApprovalText(approval), nil)
}

func approvalStatus(approval core.Approval, request core.ApprovalResolveRequest) string {
	switch {
	case !request.Approved:
		return "Denied"
	case request.Always == permission.ScopeSession && approval.Suggestion != nil:
		return "Always allowed in this session: " + approval.Suggestion.String()
	case request.Always == permission.ScopeGlobal && approval.Suggestion != nil:
		return "Always allowed everywhere: " + approval.Suggestion.String()
	default:
		return "Approved"
	}
}
