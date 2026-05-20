package telegram

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
)

func (w *Worker) renderVoiceToolResultUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	if state == nil {
		return nil
	}
	if state.voiceResults == nil {
		state.voiceResults = map[string]int64{}
	}
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
			sent, err := w.sendGeneratedSpeech(ctx, target, response)
			if err != nil {
				return err
			}
			state.voiceResults[key] = sent.MessageID
		}
	}
	return nil
}

func isTextToSpeechToolResult(result *core.ToolResultPart) bool {
	if result == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(result.Name), voicemodule.TextToSpeechToolID)
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
