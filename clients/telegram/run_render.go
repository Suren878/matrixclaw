package telegram

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (w *Worker) renderAssistantUpdates(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState) error {
	return w.renderAssistantUpdatesExcept(ctx, target, messages, runID, state, "")
}

func (w *Worker) renderAssistantProgressUpdates(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState) error {
	stream, _ := activeAssistantStreamMessage(messages, runID)
	return w.renderAssistantUpdatesExcept(silentTelegramDelivery(ctx), target, messages, runID, state, stream.ID)
}

func (w *Worker) renderAssistantUpdatesExcept(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState, streamMessageID string) error {
	streamMessageID = strings.TrimSpace(streamMessageID)
	lastAssistantID := ""
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) == strings.TrimSpace(runID) && message.Role == transcript.MessageRoleAssistant && renderAssistantMessage(message) != "" {
			lastAssistantID = message.ID
		}
	}
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != transcript.MessageRoleAssistant {
			continue
		}
		if streamMessageID != "" && strings.TrimSpace(message.ID) == streamMessageID {
			continue
		}
		text := renderAssistantMessage(message)
		if strings.TrimSpace(text) == "" {
			continue
		}
		sent, ok := state.assistant[message.ID]
		if !ok {
			sent = sentAssistantMessage{}
		}
		deliveryCtx := ctx
		if message.ID != lastAssistantID || assistantToolSegment(message) {
			deliveryCtx = silentTelegramDelivery(ctx)
		}
		updated, err := w.sendAssistantMessage(deliveryCtx, target, sent, text)
		state.assistant[message.ID] = updated
		if err != nil {
			return err
		}
		// sendMessage persists the answer and dismisses its ephemeral draft.
		// Sending an empty draft here would create a new Thinking placeholder.
		updated.draftActive = false
		state.assistant[message.ID] = updated
	}
	return nil
}

func (w *Worker) renderAssistantStreamUpdate(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState) error {
	if !target.isChat() || target.chatID == 0 || state == nil {
		return nil
	}
	assistant, ok := activeAssistantStreamMessage(messages, runID)
	if !ok {
		return nil
	}
	text := renderAssistantMessage(assistant)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return w.streamAssistantMessage(ctx, target, state, assistant.ID, text)
}

// activeAssistantStreamMessage returns text only while it is the newest message
// in the run. Once a tool call/result or another message follows it, that text
// is a completed assistant segment and the next turn starts a new message.
func activeAssistantStreamMessage(messages []transcript.Message, runID string) (transcript.Message, bool) {
	runID = strings.TrimSpace(runID)
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if strings.TrimSpace(message.RunID) != runID {
			continue
		}
		if assistantToolSegment(message) {
			return transcript.Message{}, false
		}
		if message.Role == transcript.MessageRoleAssistant && renderAssistantMessage(message) != "" {
			return message, true
		}
		return transcript.Message{}, false
	}
	return transcript.Message{}, false
}

func assistantToolSegment(message transcript.Message) bool {
	for _, part := range message.Parts {
		if part.ToolCall != nil || (part.Finish != nil && part.Finish.Reason == "tool_calls") {
			return true
		}
	}
	return false
}

func (w *Worker) sendAssistantMessage(ctx context.Context, target chatTarget, sent sentAssistantMessage, text string) (sentAssistantMessage, error) {
	formattedChunks := formatAssistantTelegramChunks(text)
	for index, formatted := range formattedChunks {
		if index < len(sent.chunks) && sent.chunks[index].messageID != 0 {
			chunk := sent.chunks[index]
			if chunk.text == formatted.Plain {
				continue
			}
			err := w.editFormattedMessage(ctx, EditMessageTextRequest{
				ChatID:    target.chatID,
				MessageID: chunk.messageID,
			}, formatted)
			if err != nil && !isTelegramMessageNotModified(err) && !shouldFallbackTelegramEdit(err) {
				return sent, err
			}
			if err == nil || isTelegramMessageNotModified(err) {
				sent.chunks[index].text = formatted.Plain
				continue
			}
		}

		reply, err := w.sendFormattedTelegramMessage(ctx, SendMessageRequest{
			ChatID:                  target.chatID,
			SkipReplyKeyboardRemove: true,
			DisableNotification:     index > 0,
		}, formatted)
		if err != nil {
			return sent, err
		}
		chunk := sentAssistantChunk{
			messageID: reply.MessageID,
			text:      formatted.Plain,
		}
		if index < len(sent.chunks) {
			sent.chunks[index] = chunk
		} else {
			sent.chunks = append(sent.chunks, chunk)
		}
	}
	// The final response may be shorter than its provisional stream (for
	// example after sanitization). Remove stale overflow instead of leaving it.
	for len(sent.chunks) > len(formattedChunks) {
		index := len(sent.chunks) - 1
		if err := w.api.DeleteMessage(ctx, DeleteMessageRequest{ChatID: target.chatID, MessageID: sent.chunks[index].messageID}); err != nil && !isTelegramMessageAlreadyDeleted(err) {
			return sent, err
		}
		sent.chunks = sent.chunks[:index]
	}
	return sent, nil
}

func formatAssistantTelegramChunks(text string) []telegramFormattedText {
	plainChunks := splitTelegramText(text, defaultMessageLimit)
	formatted := make([]telegramFormattedText, 0, len(plainChunks))
	for _, chunk := range plainChunks {
		if value := formatTelegramText(chunk); value.Plain != "" {
			formatted = append(formatted, value)
		}
	}
	return formatted
}

// splitTelegramText cuts text into chunks of at most limit runes; a code block
// cut in two is closed at the end of one chunk and reopened in the next.
func splitTelegramText(text string, limit int) []string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" || limit <= len(codeFenceClose) {
		return nil
	}
	runes := []rune(text)
	chunks := make([]string, 0, (len(runes)+limit-1)/limit)
	for len(runes) > limit {
		split := telegramTextSplitIndex(runes, limit)
		chunk := string(runes[:split])
		rest := string(runes[split:])
		if openFence(chunk) != "" {
			split = telegramTextSplitIndex(runes, limit-len(codeFenceClose))
			chunk, rest = string(runes[:split]), string(runes[split:])
		}
		chunk = strings.TrimSpace(chunk)
		if fence := openFence(chunk); fence != "" {
			chunk += codeFenceClose
			rest = fence + "\n" + strings.TrimLeft(rest, "\n")
		} else {
			rest = strings.TrimSpace(rest)
		}
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		runes = []rune(rest)
	}
	if chunk := strings.TrimSpace(string(runes)); chunk != "" {
		chunks = append(chunks, chunk)
	}
	return chunks
}

const codeFenceClose = "\n```"

// openFence is the opening line of the code block text leaves open, if any;
// one too long to repeat is a bare fence.
func openFence(text string) string {
	open := ""
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "```") {
			continue
		}
		if open == "" {
			open = trimmed
			if len(open) > 24 {
				open = "```"
			}
		} else {
			open = ""
		}
	}
	return open
}

func telegramTextSplitIndex(runes []rune, limit int) int {
	if len(runes) <= limit {
		return len(runes)
	}
	minimum := limit * 3 / 4
	for _, separator := range []rune{'\n', ' '} {
		for index := limit; index > minimum; index-- {
			if runes[index-1] == separator {
				return index
			}
		}
	}
	return limit
}

func (w *Worker) renderApprovalUpdates(ctx context.Context, target chatTarget, approvals []core.Approval, runID string, state *runDeliveryState) error {
	for _, approval := range approvals {
		if strings.TrimSpace(approval.RunID) != strings.TrimSpace(runID) || approval.State != core.ApprovalStatePending {
			continue
		}
		if _, ok := state.approvals[approval.ID]; ok {
			continue
		}
		messageID, err := w.sendApprovalMessage(ctx, target, approval)
		if err != nil {
			return err
		}
		state.approvals[approval.ID] = messageID
	}
	return nil
}

// sendApprovalMessage asks the chat for the approval with its decision buttons.
func (w *Worker) sendApprovalMessage(ctx context.Context, target chatTarget, approval core.Approval) (int64, error) {
	reply, err := w.sendTelegramMessage(ctx, SendMessageRequest{
		ChatID:      target.chatID,
		Text:        clipTelegramText(approvalRequestText(approval)),
		ReplyMarkup: w.approvalKeyboard(target, approval),
	})
	return reply.MessageID, err
}

func approvalRequestText(approval core.Approval) string {
	return "Approval required\n\n" + renderApprovalText(approval)
}
