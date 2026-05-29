package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) handleCallbackQuery(ctx context.Context, cq *CallbackQuery) error {
	if cq == nil || cq.Message == nil || !w.allowCallback(cq) {
		return nil
	}
	telegramCtx, cancel := context.WithTimeout(context.Background(), defaultTelegramHTTPTimeout)
	defer cancel()
	target := targetFromMessage(cq.Message)
	_ = w.api.AnswerCallbackQuery(telegramCtx, AnswerCallbackQueryRequest{CallbackQueryID: cq.ID})

	if resolved := w.resolveCallbackData(cq.Data); resolved != cq.Data {
		copy := *cq
		copy.Data = resolved
		cq = &copy
	}

	switch {
	case strings.HasPrefix(cq.Data, cbPicker):
		return w.handlePickerCallback(telegramCtx, target, cq)
	case strings.HasPrefix(cq.Data, cbPickerPage):
		return w.handlePickerPageCallback(telegramCtx, target, cq)
	case strings.HasPrefix(cq.Data, cbApprovalOnce):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalOnce), true, false)
	case strings.HasPrefix(cq.Data, cbApprovalSession):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalSession), true, true)
	case strings.HasPrefix(cq.Data, cbApprovalDeny):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalDeny), false, false)
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
	kind, contextID, page, ok := parsePickerPageCallbackData(cq.Data)
	if !ok {
		return nil
	}
	command := controlplane.PickerPageCommand(kind, contextID)
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return w.dispatchCommandAndEditPage(ctx, target, cq.Message.MessageID, command, page)
}

func (w *Worker) dispatchPickerCommand(ctx context.Context, target chatTarget, messageID int64, command string) error {
	if isDaemonRestartCommand(command) {
		return w.dispatchRestartCommandAndEdit(ctx, target, messageID)
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
	return matchesCatalogCommand(command, commandcatalog.CommandContext, "compact confirm")
}

func (w *Worker) resolveApprovalCallback(ctx context.Context, target chatTarget, cq *CallbackQuery, approvalID string, approved bool, allowSession bool) error {
	approval, err := w.daemon(target.externalKey).ResolveApproval(ctx, approvalID, approved)
	if err != nil {
		return w.editOrSend(ctx, target, cq.Message.MessageID, fmt.Sprintf("Resolve approval failed: %v", err), nil)
	}
	status := "Denied"
	if approved {
		status = "Approved"
	}
	if approved && allowSession && canAllowSessionApproval(approval) {
		w.rememberAutoEditSession(target, approval.SessionID)
		status = "Approved for session"
	}
	if err := w.editOrSend(ctx, target, cq.Message.MessageID, status+"\n\n"+renderApprovalText(approval), nil); err != nil {
		return err
	}
	if strings.TrimSpace(approval.RunID) != "" {
		w.startMonitor(ctx, target, approval.SessionID, approval.RunID)
	}
	return nil
}
