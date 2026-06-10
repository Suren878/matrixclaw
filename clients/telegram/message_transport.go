package telegram

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

func (w *Worker) editOrSend(ctx context.Context, target chatTarget, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	_, err := w.editOrSendMessage(ctx, target, messageID, text, markup)
	return err
}

func (w *Worker) editOrSendMessage(ctx context.Context, target chatTarget, messageID int64, text string, markup *InlineKeyboardMarkup) (int64, error) {
	formatted := formatTelegramText(text)
	if target.isInline() {
		err := w.editFormattedMessage(ctx, EditMessageTextRequest{
			InlineMessageID: target.inlineMessageID,
			ReplyMarkup:     markup,
		}, formatted)
		if err == nil || isTelegramMessageNotModified(err) {
			return 0, nil
		}
		return 0, err
	}
	if target.isGuest() {
		if strings.TrimSpace(target.inlineMessageID) != "" {
			err := w.editFormattedMessage(ctx, EditMessageTextRequest{
				InlineMessageID: target.inlineMessageID,
				ReplyMarkup:     markup,
			}, formatted)
			return 0, err
		}
		sent, err := w.answerFormattedGuestQuery(ctx, target, formatted, markup)
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(sent.InlineMessageID) != "" {
			target.inlineMessageID = sent.InlineMessageID
		}
		return 0, nil
	}
	if messageID > 0 {
		err := w.editFormattedMessage(ctx, EditMessageTextRequest{
			ChatID:      target.chatID,
			MessageID:   messageID,
			ReplyMarkup: markup,
		}, formatted)
		if err == nil || isTelegramMessageNotModified(err) {
			return messageID, nil
		}
		if !shouldFallbackTelegramEdit(err) {
			return 0, err
		}
	}
	sent, err := w.sendFormattedTelegramMessage(ctx, SendMessageRequest{
		ChatID:      target.chatID,
		ReplyMarkup: markup,
	}, formatted)
	if err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

func isTelegramMessageNotModified(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Description), "message is not modified")
}

func shouldFallbackTelegramEdit(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.ErrorCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusBadRequest {
		description := strings.ToLower(apiErr.Description)
		return strings.Contains(description, "message to edit not found") ||
			strings.Contains(description, "message can't be edited") ||
			strings.Contains(description, "message is too old") ||
			strings.Contains(description, "message_id_invalid")
	}
	return apiErr.ErrorCode == http.StatusConflict || apiErr.StatusCode == http.StatusConflict
}

func isTelegramParseError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	description := strings.ToLower(apiErr.Description)
	return strings.Contains(description, "can't parse entities") ||
		strings.Contains(description, "can't find end of the entity") ||
		strings.Contains(description, "parse")
}

func (w *Worker) sendText(ctx context.Context, target chatTarget, text string) error {
	if target.isInline() {
		formatted := formatTelegramText(clipTelegramText(text))
		err := w.editFormattedMessage(ctx, EditMessageTextRequest{
			InlineMessageID: target.inlineMessageID,
		}, formatted)
		if err == nil || isTelegramMessageNotModified(err) {
			return nil
		}
		return err
	}
	if target.isGuest() {
		formatted := formatTelegramText(clipTelegramText(text))
		_, err := w.answerFormattedGuestQuery(ctx, target, formatted, nil)
		return err
	}
	formatted := formatTelegramText(text)
	return w.sendFormattedMessage(ctx, SendMessageRequest{
		ChatID: target.chatID,
	}, formatted)
}

func (w *Worker) sendFormattedMessage(ctx context.Context, req SendMessageRequest, formatted telegramFormattedText) error {
	_, err := w.sendFormattedTelegramMessage(ctx, req, formatted)
	return err
}

func (w *Worker) sendFormattedTelegramMessage(ctx context.Context, req SendMessageRequest, formatted telegramFormattedText) (SentMessage, error) {
	req.Text = formatted.Text
	req.ParseMode = formatted.ParseMode
	reply, err := w.sendTelegramMessage(ctx, req)
	if isTelegramParseError(err) {
		req.Text = formatted.Plain
		req.ParseMode = ""
		reply, err = w.sendTelegramMessage(ctx, req)
	}
	return reply, err
}

func (w *Worker) sendFormattedTelegramDraft(ctx context.Context, req SendMessageDraftRequest, formatted telegramFormattedText) error {
	req.Text = formatted.Text
	req.ParseMode = formatted.ParseMode
	err := w.sendTelegramDraft(ctx, req)
	if isTelegramParseError(err) {
		req.Text = formatted.Plain
		req.ParseMode = ""
		err = w.sendTelegramDraft(ctx, req)
	}
	return err
}

func (w *Worker) answerFormattedGuestQuery(ctx context.Context, target chatTarget, formatted telegramFormattedText, markup *InlineKeyboardMarkup) (SentGuestMessage, error) {
	req := AnswerGuestQueryRequest{
		GuestQueryID: strings.TrimSpace(target.guestQueryID),
		Result: InlineQueryResultArticle{
			Type:  "article",
			ID:    "matrixclaw",
			Title: "Matrixclaw",
			InputMessageContent: InputTextMessageContent{
				MessageText: formatted.Text,
				ParseMode:   formatted.ParseMode,
			},
			ReplyMarkup: markup,
		},
	}
	reply, err := w.answerGuestQuery(ctx, req)
	if isTelegramParseError(err) {
		req.Result.InputMessageContent.MessageText = formatted.Plain
		req.Result.InputMessageContent.ParseMode = ""
		reply, err = w.answerGuestQuery(ctx, req)
	}
	return reply, err
}

func (w *Worker) editFormattedMessage(ctx context.Context, req EditMessageTextRequest, formatted telegramFormattedText) error {
	req.Text = formatted.Text
	req.ParseMode = formatted.ParseMode
	err := w.editTelegramMessage(ctx, req)
	if isTelegramParseError(err) {
		req.Text = formatted.Plain
		req.ParseMode = ""
		err = w.editTelegramMessage(ctx, req)
	}
	return err
}

func (w *Worker) sendTextForUpdate(ctx context.Context, update Update, text string) error {
	if update.Message != nil {
		return w.sendText(ctx, targetFromMessage(update.Message), text)
	}
	if update.GuestMessage != nil {
		return w.sendText(ctx, targetFromMessage(update.GuestMessage), text)
	}
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		return w.sendText(ctx, targetFromMessage(update.CallbackQuery.Message), text)
	}
	return nil
}
