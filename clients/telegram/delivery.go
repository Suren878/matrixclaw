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
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const maxTelegramDocumentBytes int64 = 50 << 20

func (w *Worker) deliverPendingRun(ctx context.Context, target chatTarget, sessionID string, runID string) error {
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	if sessionID == "" || runID == "" {
		return nil
	}
	return w.deliverPendingDeliveries(ctx, core.ClientDeliveryFilter{
		Type:        core.ClientDeliveryTypeRun,
		ExternalKey: target.externalKey,
		SessionID:   sessionID,
		RunID:       runID,
		Limit:       1,
	})
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
	state := w.runRenderState(target.externalKey, runID)
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingEvents:
		messages, err := w.runMessages(ctx, daemon, sessionID, runID, state)
		if err != nil {
			return err
		}
		text := transcript.RunReply(messages, runID)
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
	messages, err := w.runMessages(ctx, daemon, sessionID, runID, state)
	if err != nil {
		return err
	}
	if run.Status == core.RunStatusCompleted {
		caption := ""
		if assistant := transcript.RunReply(messages, runID); assistant != "" {
			text = assistant
			caption = assistant
		}
		inlineVoiceDelivered, err = w.renderInlineVoiceToolResultUpdates(ctx, target, messages, runID, state, caption)
		if err != nil {
			return err
		}
	}
	if !inlineVoiceDelivered {
		if err := w.sendText(ctx, target, text); err != nil {
			return err
		}
	}
	if err := w.acknowledgeSentDelivery(ctx, daemon, deliveryID); err != nil {
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
		messages, err := w.runMessages(ctx, daemon, sessionID, runID, w.runRenderState(target.externalKey, runID))
		if err != nil {
			return err
		}
		if assistant := transcript.RunReply(messages, runID); assistant != "" {
			text = assistant
		}
	}
	if err := w.sendText(ctx, target, text); err != nil {
		return err
	}
	if err := w.acknowledgeSentDelivery(ctx, daemon, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}

func (w *Worker) deliverChatRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	w.updateRunTypingIndicator(ctx, target, &run)
	switch run.Status {
	case core.RunStatusWaitingApproval:
		return w.deliverRunApprovals(ctx, target, daemon, run)
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingEvents:
		return w.deliverActiveRunProgress(ctx, target, daemon, run)
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}

	state := w.runRenderState(target.externalKey, runID)
	messages, err := w.runMessages(ctx, daemon, sessionID, runID, state)
	if err != nil {
		return err
	}
	if err := w.renderVoiceToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderEngineNotes(ctx, target, messages, runID, state); err != nil {
		return err
	}
	assistantCtx := ctx
	if run.Status != core.RunStatusCompleted {
		assistantCtx = silentTelegramDelivery(ctx)
	}
	if err := w.renderAssistantUpdates(assistantCtx, target, messages, runID, state); err != nil {
		return err
	}
	if run.Status == core.RunStatusCompleted {
		if err := w.offerContinue(ctx, target, run, state); err != nil {
			return err
		}
	}
	if run.Status != core.RunStatusCompleted && !state.errorSent {
		if err := w.sendText(ctx, target, renderRunStatus(run)); err != nil {
			return err
		}
		state.errorSent = true
	}
	if err := w.renderRunStatusMessage(ctx, target, daemon, run, messages, state); err != nil {
		return err
	}
	if err := w.acknowledgeSentDelivery(ctx, daemon, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}

func (w *Worker) deliverActiveRunProgress(ctx context.Context, target chatTarget, daemon *daemonclient.Client, run core.Run) error {
	state := w.runRenderState(target.externalKey, run.ID)
	messages, err := w.runMessages(ctx, daemon, run.SessionID, run.ID, state)
	if err != nil {
		return err
	}
	if err := w.renderRunStatusMessage(ctx, target, daemon, run, messages, state); err != nil {
		return err
	}
	if err := w.renderAssistantProgressUpdates(ctx, target, messages, run.ID, state); err != nil {
		return err
	}
	if err := w.renderVoiceToolResultUpdates(ctx, target, messages, run.ID, state); err != nil {
		return err
	}
	if err := w.renderEngineNotes(ctx, target, messages, run.ID, state); err != nil {
		return err
	}
	if err := w.renderAssistantStreamUpdate(ctx, target, messages, run.ID, state); err != nil {
		if IsRetryable(err) {
			return err
		}
		log.Printf("telegram: assistant stream update failed chat=%d run=%s: %v", target.chatID, run.ID, err)
	}
	return nil
}

func (w *Worker) deliverRunApprovals(ctx context.Context, target chatTarget, daemon *daemonclient.Client, run core.Run) error {
	state := w.runRenderState(target.externalKey, run.ID)
	messages, err := w.runMessages(ctx, daemon, run.SessionID, run.ID, state)
	if err != nil {
		return err
	}
	// Finish the current editable assistant segment before placing an approval
	// below it. Resumed model output will start a new message after the approval.
	if err := w.renderAssistantUpdates(silentTelegramDelivery(ctx), target, messages, run.ID, state); err != nil {
		return err
	}
	approvals, err := daemon.ListApprovals(ctx, run.SessionID, core.ApprovalStatePending)
	if err != nil {
		return err
	}
	if err := w.renderApprovalUpdates(ctx, target, approvals, run.ID, state); err != nil {
		return err
	}
	return w.renderRunStatusMessage(ctx, target, daemon, run, messages, state)
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
		if ctx.Err() != nil || retryableDeliveryError(err) {
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
	return w.acknowledgeSentDelivery(ctx, w.daemon(target.externalKey), delivery.ID)
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
		approvals:         map[string]int64{},
		voiceResults:      map[string]int64{},
		voiceFingerprints: map[string]int64{},
		notes:             map[string]struct{}{},
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

// activeRunRenderState is the render state of a run whose delivery is in
// progress, or a throwaway one.
func (w *Worker) activeRunRenderState(externalKey string, runID string) *runDeliveryState {
	w.mu.Lock()
	defer w.mu.Unlock()
	if state := w.states[runRenderStateKey(externalKey, runID)]; state != nil {
		return state
	}
	return newRunDeliveryState()
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

// deliverNotice sends a notice's text to its chat.
func (w *Worker) deliverNotice(ctx context.Context, daemon *daemonclient.Client, delivery core.ClientDelivery) error {
	target, ok := targetFromClientDelivery(delivery)
	if !ok || strings.TrimSpace(delivery.Summary) == "" {
		return daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
	}
	var payload core.NoticeDeliveryPayload
	if len(delivery.Payload) > 0 && json.Unmarshal(delivery.Payload, &payload) != nil {
		return daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
	}
	var err error
	if payload.Replace {
		err = w.editOrSend(ctx, target, target.messageID, telegramPersonaText(delivery.Summary), nil)
	} else {
		err = w.sendText(ctx, target, delivery.Summary)
	}
	if err != nil {
		return err
	}
	return w.acknowledgeSentDelivery(ctx, daemon, delivery.ID)
}

// deliverApproval asks the chat for a background subagent's approval, unless
// it was decided meanwhile.
func (w *Worker) deliverApproval(ctx context.Context, daemon *daemonclient.Client, delivery core.ClientDelivery) error {
	target, ok := targetFromClientDelivery(delivery)
	var payload core.ApprovalDeliveryPayload
	if !ok || !target.isChat() || json.Unmarshal(delivery.Payload, &payload) != nil {
		return daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
	}
	approvals, err := w.daemon(target.externalKey).ListApprovals(ctx, delivery.SessionID, core.ApprovalStatePending)
	if err != nil {
		return err
	}
	// A delivery of the approval's run in progress may have asked already.
	state := w.activeRunRenderState(target.externalKey, delivery.RunID)
	for _, approval := range approvals {
		if approval.ID != payload.ApprovalID {
			continue
		}
		if _, asked := state.approvals[approval.ID]; asked {
			break
		}
		messageID, err := w.sendApprovalMessage(ctx, target, approval)
		if err != nil {
			return err
		}
		state.approvals[approval.ID] = messageID
	}
	return w.acknowledgeSentDelivery(ctx, daemon, delivery.ID)
}
