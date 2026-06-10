package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
)

func (w *Worker) renderVoiceToolResultUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	_, err := w.renderVoiceToolResultUpdatesWithSender(messages, runID, state, func(response voicemodule.TextToSpeechResponse) (SentMessage, error) {
		return w.sendGeneratedSpeech(ctx, target, response)
	})
	return err
}

func (w *Worker) renderInlineVoiceToolResultUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState, caption string) (bool, error) {
	uploadTarget, ok := targetFromTelegramExternalKey(target.externalKey)
	if !ok || uploadTarget.chatID == 0 || uploadTarget.isInline() || uploadTarget.isGuest() {
		return false, nil
	}
	delivered := false
	_, err := w.renderVoiceToolResultUpdatesWithSender(messages, runID, state, func(response voicemodule.TextToSpeechResponse) (SentMessage, error) {
		sent, err := w.editInlineGeneratedSpeech(ctx, target, uploadTarget, response, caption)
		if err == nil {
			delivered = true
		}
		return sent, err
	})
	if err != nil {
		if IsRetryable(err) {
			return delivered, err
		}
		logInlineVoiceDeliveryFailure(target, runID, err)
		return false, nil
	}
	return delivered, nil
}

type generatedSpeechSender func(response voicemodule.TextToSpeechResponse) (SentMessage, error)

func (w *Worker) renderVoiceToolResultUpdatesWithSender(messages []core.Message, runID string, state *runDeliveryState, send generatedSpeechSender) (bool, error) {
	if state == nil {
		return false, nil
	}
	if state.voiceResults == nil {
		state.voiceResults = map[string]int64{}
	}
	if state.voiceFingerprints == nil {
		state.voiceFingerprints = map[string]int64{}
	}
	delivered := false
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != core.MessageRoleTool {
			continue
		}
		for _, part := range message.Parts {
			if part.ToolResult == nil || !isTextToSpeechToolResult(part.ToolResult) {
				continue
			}
			key := voiceToolResultKey(message, part.ToolResult)
			if _, sent := state.voiceResults[key]; sent {
				continue
			}
			if part.ToolResult.IsError {
				state.voiceResults[key] = 0
				continue
			}
			response, ok := textToSpeechToolResponse(part.ToolResult)
			if !ok {
				continue
			}
			fingerprint := textToSpeechResponseFingerprint(response)
			if fingerprint != "" {
				if sentID, sent := state.voiceFingerprints[fingerprint]; sent {
					state.voiceResults[key] = sentID
					continue
				}
			}
			sent, err := send(response)
			if err != nil {
				return delivered, err
			}
			delivered = true
			state.voiceResults[key] = sent.MessageID
			if fingerprint != "" {
				state.voiceFingerprints[fingerprint] = sent.MessageID
			}
		}
	}
	return delivered, nil
}

func logInlineVoiceDeliveryFailure(target chatTarget, runID string, err error) {
	if err == nil {
		return
	}
	log.Printf("telegram: inline voice delivery failed user=%s inline_message_id=%s run=%s: %v", target.externalKey, target.inlineMessageID, runID, err)
}

func isTextToSpeechToolResult(result *core.ToolResultPart) bool {
	if result == nil {
		return false
	}
	return isTextToSpeechToolName(result.Name)
}

func isTextToSpeechToolName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), voicemodule.TextToSpeechToolID)
}

func textToSpeechToolResponse(result *core.ToolResultPart) (voicemodule.TextToSpeechResponse, bool) {
	if result == nil || len(result.Metadata) == 0 {
		return voicemodule.TextToSpeechResponse{}, false
	}
	var metadata struct {
		Type          string `json:"type"`
		ContentBase64 string `json:"content_base64"`
		MIMEType      string `json:"mime_type"`
		FileName      string `json:"file_name"`
	}
	if err := json.Unmarshal(result.Metadata, &metadata); err == nil && strings.TrimSpace(metadata.ContentBase64) != "" {
		response := voicemodule.TextToSpeechResponse{
			ContentBase64: metadata.ContentBase64,
			MIMEType:      metadata.MIMEType,
			FileName:      metadata.FileName,
		}
		if strings.TrimSpace(response.MIMEType) == "" {
			response.MIMEType = strings.TrimSpace(result.MIMEType)
		}
		return response, true
	}
	var response voicemodule.TextToSpeechResponse
	if err := json.Unmarshal(result.Metadata, &response); err != nil {
		return voicemodule.TextToSpeechResponse{}, false
	}
	if strings.TrimSpace(response.ContentBase64) == "" {
		return voicemodule.TextToSpeechResponse{}, false
	}
	if strings.TrimSpace(response.MIMEType) == "" {
		response.MIMEType = strings.TrimSpace(result.MIMEType)
	}
	return response, true
}

func voiceToolResultKey(message core.Message, result *core.ToolResultPart) string {
	if result == nil {
		return strings.TrimSpace(message.ID)
	}
	key := strings.TrimSpace(message.ID)
	if callID := strings.TrimSpace(result.ToolCallID); callID != "" {
		key += ":" + callID
	}
	return key
}

func textToSpeechResponseFingerprint(response voicemodule.TextToSpeechResponse) string {
	content := strings.TrimSpace(response.ContentBase64)
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		content,
		strings.TrimSpace(response.MIMEType),
		strings.TrimSpace(response.FileName),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}
