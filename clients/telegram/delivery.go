package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

const maxTelegramDocumentBytes int64 = 50 << 20

func (w *Worker) deliverPendingRuns(ctx context.Context) error {
	return w.deliverPendingRunDeliveries(ctx, core.ClientDeliveryTypeRun, core.ClientDeliveryFilter{})
}

func (w *Worker) deliverPendingRun(ctx context.Context, target chatTarget, sessionID string, runID string) error {
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	if sessionID == "" || runID == "" {
		return nil
	}
	return w.deliverPendingRunDeliveries(ctx, core.ClientDeliveryTypeRun, core.ClientDeliveryFilter{
		ExternalKey: target.externalKey,
		SessionID:   sessionID,
		RunID:       runID,
		Limit:       1,
	})
}

func (w *Worker) deliverPendingRunDeliveries(ctx context.Context, deliveryType string, filter core.ClientDeliveryFilter) error {
	w.delivery.Lock()
	defer w.delivery.Unlock()

	deliveryType = strings.TrimSpace(deliveryType)
	if deliveryType == "" {
		return nil
	}
	daemon := w.daemon("")
	filter.Client = w.config.ClientName
	filter.Type = deliveryType
	filter.Status = core.ClientDeliveryStatusPending
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	deliveries, err := daemon.ListClientDeliveries(ctx, filter)
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if err := w.deliverPendingRunDelivery(ctx, daemon, delivery); err != nil {
			if IsRetryable(err) {
				return err
			}
			log.Printf("telegram: run delivery %s failed: %v", delivery.ID, err)
			_ = daemon.FailClientDelivery(ctx, delivery.ID, err.Error())
		}
	}
	return nil
}

func (w *Worker) deliverPendingRunDelivery(ctx context.Context, daemon *daemonclient.Client, delivery core.ClientDelivery) error {
	target, ok := targetFromClientDelivery(delivery)
	if !ok {
		return daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
	}
	sessionID, runID := runDeliveryRun(delivery)
	if sessionID == "" || runID == "" {
		return daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
	}
	if target.isInline() {
		return w.deliverInlineRunDelivery(ctx, target, sessionID, runID, delivery.ID)
	}
	if target.isGuest() {
		return w.deliverGuestRunDelivery(ctx, target, sessionID, runID, delivery.ID)
	}
	return w.deliverChatRunDelivery(ctx, target, sessionID, runID, delivery.ID)
}

func (w *Worker) deliverInlineRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning:
		messages, err := daemon.ListMessages(ctx, sessionID, 0)
		if err != nil {
			return err
		}
		text := latestAssistantDraftText(messages, runID)
		if strings.TrimSpace(text) == "" {
			text = renderRunStatus(run)
		}
		return w.sendText(ctx, target, text)
	case core.RunStatusWaitingApproval:
		return w.sendText(ctx, target, "Approval required. Open the private Matrixclaw chat to approve or deny the request.")
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}

	text := renderRunStatus(run)
	inlineVoiceDelivered := false
	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	if run.Status == core.RunStatusCompleted {
		caption := ""
		if assistant := lastAssistantMessageText(messages, runID); assistant != "" {
			text = assistant
			caption = assistant
		}
		inlineVoiceDelivered, err = w.renderInlineVoiceToolResultUpdates(ctx, target, messages, runID, w.runRenderState(target.externalKey, runID), caption)
		if err != nil {
			return err
		}
	}
	if !inlineVoiceDelivered {
		if err := w.sendText(ctx, target, text); err != nil {
			return err
		}
	}
	if err := daemon.AcknowledgeClientDelivery(ctx, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}

func (w *Worker) deliverGuestRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}
	text := renderRunStatus(run)
	if run.Status == core.RunStatusCompleted {
		messages, err := daemon.ListMessages(ctx, sessionID, 0)
		if err != nil {
			return err
		}
		if assistant := lastAssistantMessageText(messages, runID); assistant != "" {
			text = assistant
		}
	}
	if err := w.sendText(ctx, target, text); err != nil {
		return err
	}
	return daemon.AcknowledgeClientDelivery(ctx, deliveryID)
}

func (w *Worker) deliverChatRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case core.RunStatusWaitingApproval:
		return w.deliverRunApprovals(ctx, target, daemon, sessionID, runID)
	case core.RunStatusAccepted, core.RunStatusRunning:
		return w.deliverActiveRunProgress(ctx, target, daemon, sessionID, runID)
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}

	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	state := w.runRenderState(target.externalKey, runID)
	if err := w.renderToolCallUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderVoiceToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderAssistantUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if run.Status != core.RunStatusCompleted && len(state.assistant) == 0 {
		if err := w.sendText(ctx, target, renderRunStatus(run)); err != nil {
			return err
		}
	}
	if err := daemon.AcknowledgeClientDelivery(ctx, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}

func (w *Worker) deliverActiveRunProgress(ctx context.Context, target chatTarget, daemon *daemonclient.Client, sessionID string, runID string) error {
	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	state := w.runRenderState(target.externalKey, runID)
	if err := w.renderAssistantDraftUpdate(ctx, target, messages, runID, state); err != nil {
		if IsRetryable(err) {
			return err
		}
		log.Printf("telegram: draft update failed chat=%d run=%s: %v", target.chatID, runID, err)
	}
	return nil
}

func (w *Worker) deliverRunApprovals(ctx context.Context, target chatTarget, daemon *daemonclient.Client, sessionID string, runID string) error {
	approvals, err := daemon.ListApprovals(ctx, sessionID, core.ApprovalStatePending)
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		return nil
	}
	return w.renderApprovalUpdates(ctx, target, approvals, runID, w.runRenderState(target.externalKey, runID))
}

func lastAssistantMessageText(messages []core.Message, runID string) string {
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != core.MessageRoleAssistant {
			continue
		}
		if text := renderAssistantMessage(message); text != "" {
			return text
		}
	}
	return ""
}

func (w *Worker) deliverPendingDocuments(ctx context.Context) error {
	daemon := w.daemon("")
	deliveries, err := daemon.ListClientDeliveries(ctx, core.ClientDeliveryFilter{
		Client: w.config.ClientName,
		Type:   core.ClientDeliveryTypeDocument,
		Status: core.ClientDeliveryStatusPending,
		Limit:  20,
	})
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if err := w.deliverDocument(ctx, delivery); err != nil {
			log.Printf("telegram: document delivery %s failed: %v", delivery.ID, err)
		}
	}
	return nil
}

func (w *Worker) deliverDocument(ctx context.Context, delivery core.ClientDelivery) error {
	target, ok := targetFromClientDelivery(delivery)
	if !ok {
		return w.daemon("").FailClientDelivery(ctx, delivery.ID, "telegram target is missing")
	}
	if !target.isChat() || target.chatID == 0 {
		return w.failDocumentDelivery(ctx, target, delivery, "document delivery requires a private chat")
	}
	var payload core.DocumentDeliveryPayload
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		return w.failDocumentDelivery(ctx, target, delivery, fmt.Sprintf("invalid payload: %v", err))
	}
	payload.StoragePath = strings.TrimSpace(payload.StoragePath)
	if payload.StoragePath == "" {
		return w.failDocumentDelivery(ctx, target, delivery, "storage_path is empty")
	}

	content, fileName, mimeType, err := w.readDeliveryDocument(ctx, target, payload)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return w.failDocumentDelivery(ctx, target, delivery, err.Error())
	}
	if len(content) == 0 {
		return w.failDocumentDelivery(ctx, target, delivery, "file is empty")
	}
	if int64(len(content)) > maxTelegramDocumentBytes {
		return w.failDocumentDelivery(ctx, target, delivery, fmt.Sprintf("file is too large: %d bytes", len(content)))
	}
	if err := w.api.SendChatAction(ctx, SendChatActionRequest{
		ChatID: target.chatID,
		Action: "upload_document",
	}); err != nil {
		log.Printf("telegram: upload_document indicator failed: %v", err)
	}
	sent, err := w.api.SendDocument(ctx, SendDocumentRequest{
		ChatID:   target.chatID,
		Document: content,
		FileName: fileName,
		Caption:  telegramDocumentCaption(payload.Caption),
		MIMEType: mimeType,
	})
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		if IsRetryable(err) {
			return err
		}
		return w.failDocumentDelivery(ctx, target, delivery, err.Error())
	}
	log.Printf("telegram: sent document delivery=%s chat=%d message=%d file=%s mime=%s bytes=%d", delivery.ID, target.chatID, sent.MessageID, fileName, mimeType, len(content))
	return w.daemon(target.externalKey).AcknowledgeClientDelivery(ctx, delivery.ID)
}

func (w *Worker) failDocumentDelivery(ctx context.Context, target chatTarget, delivery core.ClientDelivery, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "unknown error"
	}
	if target.chatID != 0 {
		_ = w.sendText(ctx, target, "File delivery failed: "+message+".")
	}
	return w.daemon(target.externalKey).FailClientDelivery(ctx, delivery.ID, message)
}

func (w *Worker) readDeliveryDocument(ctx context.Context, target chatTarget, payload core.DocumentDeliveryPayload) ([]byte, string, string, error) {
	daemon := w.daemon(target.externalKey)
	if payload.Temporary {
		result, err := daemon.ReadTemporaryStorageFileBytes(ctx, payload.StoragePath)
		if err != nil {
			return nil, "", "", err
		}
		content, err := result.ContentBytes()
		if err != nil {
			return nil, "", "", err
		}
		return content, documentFileName(payload.FileName, result.File.Title, result.File.Path), firstNonEmpty(payload.MIMEType, result.File.MIMEType, "application/octet-stream"), nil
	}
	result, err := daemon.ReadStorageFileBytes(ctx, payload.StoragePath)
	if err != nil {
		return nil, "", "", err
	}
	content, err := result.ContentBytes()
	if err != nil {
		return nil, "", "", err
	}
	return content, documentFileName(payload.FileName, result.File.Title, result.File.Path), firstNonEmpty(payload.MIMEType, result.File.MIMEType, "application/octet-stream"), nil
}

func targetFromClientDelivery(delivery core.ClientDelivery) (chatTarget, bool) {
	if target, ok := targetFromDeliveryAddress(delivery.Address, delivery.ExternalKey); ok {
		return target, true
	}
	return chatTarget{}, false
}

func runDeliveryRun(delivery core.ClientDelivery) (string, string) {
	return strings.TrimSpace(delivery.SessionID), strings.TrimSpace(delivery.RunID)
}

func newRunDeliveryState() *runDeliveryState {
	return &runDeliveryState{
		assistant:         map[string]sentAssistantMessage{},
		drafts:            map[string]sentAssistantDraft{},
		approvals:         map[string]int64{},
		toolCalls:         map[string]sentToolCallStatus{},
		voiceResults:      map[string]int64{},
		voiceFingerprints: map[string]int64{},
	}
}

func (w *Worker) runRenderState(externalKey string, runID string) *runDeliveryState {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := runRenderStateKey(externalKey, runID)
	state := w.states[key]
	if state == nil {
		state = newRunDeliveryState()
		w.states[key] = state
	}
	return state
}

func (w *Worker) clearRunRenderState(externalKey string, runID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.states, runRenderStateKey(externalKey, runID))
}

func runRenderStateKey(externalKey string, runID string) string {
	return strings.TrimSpace(externalKey) + ":" + strings.TrimSpace(runID)
}

func documentFileName(values ...string) string {
	name := firstNonEmpty(values...)
	if name == "" {
		name = "matrixclaw-file"
	}
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) {
		return "matrixclaw-file"
	}
	return name
}

func telegramDocumentCaption(caption string) string {
	runes := []rune(strings.TrimSpace(caption))
	if len(runes) <= 1024 {
		return string(runes)
	}
	return string(runes[:1021]) + "..."
}
