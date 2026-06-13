package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

const inlineCallbackPrefix = "inline:"
const inlineResultIDPrefix = "matrixclaw:"

func (w *Worker) handleInlineQuery(ctx context.Context, query *InlineQuery) error {
	if query == nil || !w.allowInlineUser(query.From) {
		return nil
	}
	text := strings.TrimSpace(query.Query)
	if text == "" {
		return w.api.AnswerInlineQuery(ctx, AnswerInlineQueryRequest{
			InlineQueryID: strings.TrimSpace(query.ID),
			Results:       []InlineQueryResultArticle{},
			CacheTime:     1,
			IsPersonal:    true,
		})
	}
	requestText := inlineTextWithLocation(text, query.Location)
	callbackData := w.rememberInlineRequest(query.ID, requestText)
	result := InlineQueryResultArticle{
		Type:        "article",
		ID:          inlineResultID(callbackData),
		Title:       "Tap to ask Matrixclaw",
		Description: clipInlineDescription(text),
		InputMessageContent: InputTextMessageContent{
			MessageText: clipTelegramText(inlinePlaceholderText(text)),
			ParseMode:   "",
		},
		ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
			{Text: "Get answer", CallbackData: callbackData},
		}}},
	}
	return w.api.AnswerInlineQuery(ctx, AnswerInlineQueryRequest{
		InlineQueryID: strings.TrimSpace(query.ID),
		Results:       []InlineQueryResultArticle{result},
		CacheTime:     1,
		IsPersonal:    true,
	})
}

func (w *Worker) handleChosenInlineResult(ctx context.Context, chosen *ChosenInlineResult) error {
	if chosen == nil || !w.allowInlineUser(chosen.From) {
		return nil
	}
	inlineMessageID := strings.TrimSpace(chosen.InlineMessageID)
	text := w.inlineChosenRequestText(chosen)
	if inlineMessageID == "" || text == "" {
		log.Printf("telegram: chosen_inline_result ignored user=%d inline_message=%t query_bytes=%d", telegramUserID(chosen.From), inlineMessageID != "", len([]byte(text)))
		return nil
	}
	if w.inlineMessageStarted(inlineMessageID) {
		return nil
	}
	target := targetFromChosenInlineResult(chosen)
	started, err := w.startInlineUserMessage(ctx, target, text)
	if started {
		w.markInlineMessageStarted(inlineMessageID)
	}
	return err
}

func (w *Worker) inlineChosenRequestText(chosen *ChosenInlineResult) string {
	if chosen == nil {
		return ""
	}
	if token := inlineTokenFromResultID(chosen.ResultID); token != "" {
		if text := w.inlineRequestText(token); text != "" {
			return text
		}
	}
	return inlineTextWithLocation(chosen.Query, chosen.Location)
}

func (w *Worker) handleInlineCallback(ctx context.Context, cq *CallbackQuery) error {
	if cq == nil || !w.allowInlineUser(cq.From) {
		return nil
	}
	telegramCtx, cancel := context.WithTimeout(context.Background(), defaultTelegramHTTPTimeout)
	defer cancel()

	inlineMessageID := strings.TrimSpace(cq.InlineMessageID)
	token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cq.Data), inlineCallbackPrefix))
	text := w.inlineRequestText(token)
	if inlineMessageID == "" || text == "" {
		_ = w.api.AnswerCallbackQuery(telegramCtx, AnswerCallbackQueryRequest{CallbackQueryID: cq.ID})
		if inlineMessageID != "" {
			target := targetFromInlineCallback(cq)
			return w.sendText(ctx, target, "Inline request expired. Type @bot again and choose the result.")
		}
		return nil
	}
	if w.inlineMessageStarted(inlineMessageID) {
		return w.api.AnswerCallbackQuery(telegramCtx, AnswerCallbackQueryRequest{
			CallbackQueryID: cq.ID,
			Text:            "Already running.",
		})
	}
	_ = w.api.AnswerCallbackQuery(telegramCtx, AnswerCallbackQueryRequest{CallbackQueryID: cq.ID})
	target := targetFromInlineCallback(cq)
	started, err := w.startInlineUserMessage(ctx, target, text)
	if started {
		w.markInlineMessageStarted(inlineMessageID)
	}
	return err
}

func (w *Worker) sendInlineUserMessage(ctx context.Context, target chatTarget, text string) error {
	_, err := w.startInlineUserMessage(ctx, target, text)
	return err
}

func (w *Worker) startInlineUserMessage(ctx context.Context, target chatTarget, text string) (bool, error) {
	err := w.sendInlineUserMessageInSession(ctx, target, text, "")
	if err == nil {
		return true, nil
	}
	if !daemonclient.IsAPIStatus(err, http.StatusConflict) {
		return false, w.sendText(ctx, target, fmt.Sprintf("Matrixclaw request failed: %v", err))
	}
	sessionID := w.inlineFallbackSessionID(ctx, target.externalKey)
	if sessionID == "" {
		return false, w.sendText(ctx, target, "Matrixclaw needs a session selection in the private chat before inline requests can run.")
	}
	if retryErr := w.sendInlineUserMessageInSession(ctx, target, text, sessionID); retryErr != nil {
		return false, w.sendText(ctx, target, fmt.Sprintf("Matrixclaw request failed: %v", retryErr))
	}
	return true, nil
}

func (w *Worker) sendInlineUserMessageInSession(ctx context.Context, target chatTarget, text string, sessionID string) error {
	daemon := w.daemon(target.externalKey)
	result, err := daemon.SendMessagePartsModeWithDelivery(
		ctx,
		strings.TrimSpace(sessionID),
		inlineRunPrompt(text),
		nil,
		w.config.WorkingDir,
		"",
		encodeDeliveryAddress(deliveryAddressFromTarget(target, 0)),
	)
	if err != nil {
		return err
	}
	if err := w.deliverPendingRun(ctx, target, result.SessionID, result.Run.ID); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func (w *Worker) inlineFallbackSessionID(ctx context.Context, externalKey string) string {
	daemon := w.daemon(externalKey)
	if binding, err := daemon.CurrentBinding(ctx); err == nil && strings.TrimSpace(binding.SessionID) != "" {
		return strings.TrimSpace(binding.SessionID)
	}
	sessions, err := daemon.ListSessions(ctx)
	if err != nil {
		log.Printf("telegram: inline fallback session lookup failed: %v", err)
		return ""
	}
	w.rememberTelegramSessions(sessions)
	for _, session := range sessions {
		if session.Hidden {
			continue
		}
		if core.NormalizeSessionRuntime(session.RuntimeID) == core.SessionRuntimeExternalAgent ||
			core.NormalizeSessionKind(session.Kind) == core.SessionKindExternalAgent {
			continue
		}
		if strings.TrimSpace(session.ID) != "" {
			return strings.TrimSpace(session.ID)
		}
	}
	return ""
}

func targetFromInlineCallback(cq *CallbackQuery) chatTarget {
	userID := int64(0)
	if cq != nil && cq.From != nil {
		userID = cq.From.ID
	}
	target := chatTarget{
		kind:            telegramTargetInline,
		chatID:          userID,
		inlineMessageID: strings.TrimSpace(cq.InlineMessageID),
	}
	if userID != 0 {
		target.externalKey = telegramExternalKey(userID)
	}
	return target
}

func targetFromChosenInlineResult(chosen *ChosenInlineResult) chatTarget {
	userID := int64(0)
	if chosen != nil && chosen.From != nil {
		userID = chosen.From.ID
	}
	target := chatTarget{
		kind:            telegramTargetInline,
		chatID:          userID,
		inlineMessageID: strings.TrimSpace(chosen.InlineMessageID),
	}
	if userID != 0 {
		target.externalKey = telegramExternalKey(userID)
	}
	return target
}

func (w *Worker) rememberInlineRequest(queryID string, text string) string {
	text = strings.TrimSpace(text)
	token := inlineRequestToken(queryID, text)
	w.mu.Lock()
	if w.inline == nil {
		w.inline = map[string]string{}
	}
	w.inline[token] = text
	snapshot := copyInlineRequests(w.inline)
	w.mu.Unlock()
	if err := writeInlineRequestCache(w.config.InlineCachePath, snapshot); err != nil {
		log.Printf("telegram: persist inline request cache failed: %v", err)
	}
	return inlineCallbackPrefix + token
}

func (w *Worker) inlineRequestText(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	w.mu.Lock()
	if text := strings.TrimSpace(w.inline[token]); text != "" {
		w.mu.Unlock()
		return text
	}
	w.mu.Unlock()

	cached := readInlineRequestCache(w.config.InlineCachePath)
	text := strings.TrimSpace(cached[token])
	if text == "" {
		return ""
	}
	w.mu.Lock()
	if w.inline == nil {
		w.inline = map[string]string{}
	}
	w.inline[token] = text
	w.mu.Unlock()
	return text
}

func (w *Worker) inlineMessageStarted(inlineMessageID string) bool {
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.inlineRuns[inlineMessageID]
	return ok
}

func (w *Worker) markInlineMessageStarted(inlineMessageID string) {
	inlineMessageID = strings.TrimSpace(inlineMessageID)
	if inlineMessageID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inlineRuns == nil {
		w.inlineRuns = map[string]struct{}{}
	}
	w.inlineRuns[inlineMessageID] = struct{}{}
}

func inlineRequestToken(queryID string, text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(queryID) + "\x00" + strings.TrimSpace(text)))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}

func inlineResultID(callbackData string) string {
	token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(callbackData), inlineCallbackPrefix))
	if token == "" {
		return "matrixclaw"
	}
	id := inlineResultIDPrefix + token
	if len(id) > 64 {
		return "matrixclaw"
	}
	return id
}

func inlineTokenFromResultID(resultID string) string {
	resultID = strings.TrimSpace(resultID)
	token, ok := strings.CutPrefix(resultID, inlineResultIDPrefix)
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func inlineTextWithLocation(text string, location *Location) string {
	text = strings.TrimSpace(text)
	if location == nil {
		return text
	}
	locationText := telegramLocationPrompt(*location)
	if text == "" {
		return locationText
	}
	return text + "\n\n" + locationText
}

func copyInlineRequests(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for token, text := range source {
		token = strings.TrimSpace(token)
		text = strings.TrimSpace(text)
		if token != "" && text != "" {
			out[token] = text
		}
	}
	return out
}

func readInlineRequestCache(path string) map[string]string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload struct {
		Requests map[string]string `json:"requests"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return copyInlineRequests(payload.Requests)
}

func writeInlineRequestCache(path string, requests map[string]string) error {
	path = strings.TrimSpace(path)
	if path == "" || len(requests) == 0 {
		return nil
	}
	payload := struct {
		Requests map[string]string `json:"requests"`
	}{Requests: requests}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func telegramUserID(user *User) int64 {
	if user == nil {
		return 0
	}
	return user.ID
}

func inlineRunPrompt(query string) string {
	query = strings.TrimSpace(query)
	return "Telegram inline request. Return a concise answer ready to paste into the current chat. Do not mention internal routing, sessions, tools, or Matrixclaw unless the user asked about them.\n\nRequest: " + query
}

func inlinePlaceholderText(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "Thinking..."
	}
	return "Thinking about:\n\n" + query
}

func clipInlineDescription(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= 96 {
		return text
	}
	return strings.TrimSpace(string(runes[:93])) + "..."
}
