package telegram

import (
	"context"
	"hash/fnv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (w *Worker) renderAssistantUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != core.MessageRoleAssistant {
			continue
		}
		text := renderAssistantMessage(message)
		formatted := formatTelegramText(text)
		if formatted.Plain == "" {
			continue
		}
		sent, ok := state.assistant[message.ID]
		if !ok {
			sent = sentAssistantMessage{}
		}
		updated, err := w.sendAssistantMessage(ctx, target, sent, formatted)
		if err != nil {
			return err
		}
		state.assistant[message.ID] = updated
	}
	return nil
}

func (w *Worker) renderAssistantDraftUpdate(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	if !target.isChat() || target.chatID == 0 || state == nil {
		return nil
	}
	draftKey := strings.TrimSpace(runID)
	draftText := clipTelegramText(latestAssistantDraftText(messages, runID))
	if strings.TrimSpace(draftText) == "" {
		draftText = defaultThinkingDraftText
	}
	formatted := formatTelegramText(draftText)
	sent := state.drafts[draftKey]
	if sent.text == formatted.Plain && !sent.sentAt.IsZero() && time.Since(sent.sentAt) < defaultDraftRefresh {
		return nil
	}
	if err := w.sendFormattedTelegramDraft(ctx, SendMessageDraftRequest{
		ChatID:  target.chatID,
		DraftID: telegramDraftID(runID),
	}, formatted); err != nil {
		return err
	}
	state.drafts[draftKey] = sentAssistantDraft{
		text:   formatted.Plain,
		sentAt: time.Now(),
	}
	return nil
}

func latestAssistantDraftText(messages []core.Message, runID string) string {
	runID = strings.TrimSpace(runID)
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if strings.TrimSpace(message.RunID) != runID || message.Role != core.MessageRoleAssistant {
			continue
		}
		if text := renderAssistantMessage(message); text != "" {
			return text
		}
	}
	return ""
}

func telegramDraftID(runID string) int64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.TrimSpace(runID)))
	id := int64(hash.Sum32() & 0x7fffffff)
	if id == 0 {
		return 1
	}
	return id
}

func (w *Worker) sendAssistantMessage(ctx context.Context, target chatTarget, sent sentAssistantMessage, formatted telegramFormattedText) (sentAssistantMessage, error) {
	if sent.messageID != 0 {
		return sent, nil
	}
	reply, err := w.sendFormattedTelegramMessage(ctx, SendMessageRequest{
		ChatID: target.chatID,
	}, formatted)
	if err != nil {
		return sent, err
	}
	return sentAssistantMessage{messageID: reply.MessageID, text: formatted.Plain}, nil
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
			ChatID:      target.chatID,
			Text:        clipTelegramText("Approval required\n\n" + renderApprovalText(approval)),
			ReplyMarkup: approvalKeyboard(approval),
		})
		if err != nil {
			return err
		}
		state.approvals[approval.ID] = reply.MessageID
	}
	return nil
}
