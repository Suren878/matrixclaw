package gateway

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func appendTranscript(call *Call, raw json.RawMessage, input bool, final bool) {
	if call == nil || len(raw) == 0 {
		return
	}
	var payload realtime.TranscriptPayload
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.Text) == "" {
		return
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	if input {
		if final {
			call.currentInputTranscript = payload.Text
			call.InputTranscript = payload.Text
			return
		}
		call.currentInputTranscript += payload.Text
		call.InputTranscript = call.currentInputTranscript
	} else {
		if final {
			call.currentAssistantTranscript = payload.Text
			call.AssistantTranscript = payload.Text
			return
		}
		call.currentAssistantTranscript += payload.Text
		call.AssistantTranscript = call.currentAssistantTranscript
	}
}

func clearCurrentAssistantTranscript(call *Call) {
	if call == nil {
		return
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	call.currentAssistantTranscript = ""
	call.AssistantTranscript = joinTranscript(call.Transcript, "assistant")
}

func appendTranscriptTurn(call *Call, raw json.RawMessage, at time.Time) {
	if call == nil || len(raw) == 0 {
		return
	}
	var payload realtime.TurnFinalPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	input := strings.TrimSpace(payload.InputTranscript)
	assistant := strings.TrimSpace(payload.AssistantTranscript)
	if input == "" && assistant == "" {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	index := nextTranscriptTurnIndex(call.Transcript)
	if input != "" {
		call.Transcript = append(call.Transcript, CallTranscriptTurn{
			At:        at,
			Speaker:   "caller",
			Text:      input,
			TurnIndex: index,
		})
	}
	if assistant != "" {
		call.Transcript = append(call.Transcript, CallTranscriptTurn{
			At:        at,
			Speaker:   "assistant",
			Text:      assistant,
			TurnIndex: index,
		})
	}
	call.InputTranscript = joinTranscript(call.Transcript, "caller")
	call.AssistantTranscript = joinTranscript(call.Transcript, "assistant")
	call.currentInputTranscript = ""
	call.currentAssistantTranscript = ""
}

func joinTranscript(turns []CallTranscriptTurn, speaker string) string {
	var parts []string
	for _, turn := range turns {
		if turn.Speaker == speaker && strings.TrimSpace(turn.Text) != "" {
			parts = append(parts, strings.TrimSpace(turn.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func nextTranscriptTurnIndex(turns []CallTranscriptTurn) int {
	maxIndex := 0
	for _, turn := range turns {
		if turn.TurnIndex > maxIndex {
			maxIndex = turn.TurnIndex
		}
	}
	return maxIndex + 1
}
