package telegram

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (w *Worker) renderAssistantUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != core.MessageRoleAssistant {
			continue
		}
		formatted := formatTelegramText(renderAssistantMessage(message))
		if formatted.Plain == "" {
			continue
		}
		sent, ok := state.assistant[message.ID]
		if !ok {
			reply, err := w.sendFormattedTelegramMessage(ctx, SendMessageRequest{
				ChatID:          target.chatID,
				MessageThreadID: target.threadID,
			}, formatted)
			if err != nil {
				return err
			}
			state.assistant[message.ID] = sentAssistantMessage{
				messageID: reply.MessageID,
				text:      formatted.Plain,
			}
			continue
		}
		if sent.text == formatted.Plain {
			continue
		}
		if err := w.editFormattedMessage(ctx, EditMessageTextRequest{
			ChatID:    target.chatID,
			MessageID: sent.messageID,
		}, formatted); err != nil {
			return err
		}
		sent.text = formatted.Plain
		state.assistant[message.ID] = sent
	}
	return nil
}

func (w *Worker) renderApprovalUpdates(ctx context.Context, target chatTarget, approvals []core.Approval, runID string, state *runDeliveryState) error {
	for _, approval := range approvals {
		if strings.TrimSpace(approval.RunID) != strings.TrimSpace(runID) || approval.State != core.ApprovalStatePending {
			continue
		}
		if _, ok := state.approvals[approval.ID]; ok {
			continue
		}
		if w.autoApprovesEditApproval(target, approval) {
			if _, err := w.daemon(target.externalKey).ResolveApproval(ctx, approval.ID, true); err != nil {
				return err
			}
			state.approvals[approval.ID] = 0
			continue
		}
		reply, err := w.sendTelegramMessage(ctx, SendMessageRequest{
			ChatID:          target.chatID,
			MessageThreadID: target.threadID,
			Text:            clipTelegramText("Approval required\n\n" + renderApprovalText(approval)),
			ReplyMarkup:     approvalKeyboard(approval),
		})
		if err != nil {
			return err
		}
		state.approvals[approval.ID] = reply.MessageID
	}
	return nil
}
