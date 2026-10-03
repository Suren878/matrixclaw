package telegram

import (
	"bytes"
	"encoding/json"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// renderStateLifetime bounds how long the saved render state of a run whose
// delivery never finished is kept.
const renderStateLifetime = 7 * 24 * time.Hour

// savedRunState is what a run's render state says about the Telegram messages
// already sent for it, so a restarted worker edits them instead of sending
// them again.
type savedRunState struct {
	StartedAt         time.Time               `json:"started_at"`
	StatusMessageID   int64                   `json:"status_message_id,omitempty"`
	ErrorSent         bool                    `json:"error_sent,omitempty"`
	ContinueOffered   bool                    `json:"continue_offered,omitempty"`
	Assistant         map[string][]savedChunk `json:"assistant,omitempty"`
	Approvals         map[string]int64        `json:"approvals,omitempty"`
	VoiceResults      map[string]int64        `json:"voice_results,omitempty"`
	VoiceFingerprints map[string]int64        `json:"voice_fingerprints,omitempty"`
	Notes             []string                `json:"notes,omitempty"`
}

type savedChunk struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
}

func saveRunState(state *runDeliveryState) savedRunState {
	saved := savedRunState{
		StartedAt:         state.startedAt,
		StatusMessageID:   state.status.messageID,
		ErrorSent:         state.errorSent,
		ContinueOffered:   state.continueOffered,
		Approvals:         state.approvals,
		VoiceResults:      state.voiceResults,
		VoiceFingerprints: state.voiceFingerprints,
		Notes:             slices.Sorted(maps.Keys(state.notes)),
	}
	for id, sent := range state.assistant {
		if len(sent.chunks) == 0 {
			continue
		}
		if saved.Assistant == nil {
			saved.Assistant = map[string][]savedChunk{}
		}
		for _, chunk := range sent.chunks {
			saved.Assistant[id] = append(saved.Assistant[id], savedChunk{MessageID: chunk.messageID, Text: chunk.text})
		}
	}
	return saved
}

// restore is the render state of a run whose messages are loaded afresh.
func (saved savedRunState) restore() *runDeliveryState {
	state := newRunDeliveryState()
	state.startedAt = saved.StartedAt
	state.status.messageID = saved.StatusMessageID
	state.errorSent = saved.ErrorSent
	state.continueOffered = saved.ContinueOffered
	maps.Copy(state.approvals, saved.Approvals)
	maps.Copy(state.voiceResults, saved.VoiceResults)
	maps.Copy(state.voiceFingerprints, saved.VoiceFingerprints)
	for _, id := range saved.Notes {
		state.notes[id] = struct{}{}
	}
	for id, chunks := range saved.Assistant {
		var sent sentAssistantMessage
		for _, chunk := range chunks {
			sent.chunks = append(sent.chunks, sentAssistantChunk{messageID: chunk.MessageID, text: chunk.Text})
		}
		state.assistant[id] = sent
	}
	return state
}

// loadRenderStates reads the saved render states, without those older than
// their lifetime, and the file content they were read from.
func loadRenderStates(path string, now time.Time) (map[string]*runDeliveryState, []byte) {
	states := map[string]*runDeliveryState{}
	if strings.TrimSpace(path) == "" {
		return states, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return states, nil
	}
	var saved map[string]savedRunState
	if err := json.Unmarshal(data, &saved); err != nil {
		log.Printf("telegram: render state file ignored: %v", err)
		return states, nil
	}
	for key, run := range saved {
		if now.Sub(run.StartedAt) < renderStateLifetime {
			states[key] = run.restore()
		}
	}
	return states, data
}

// saveRenderStates writes the render states when they changed since the last
// write.
func (w *Worker) saveRenderStates() {
	path := strings.TrimSpace(w.config.RenderStatePath)
	if path == "" {
		return
	}
	w.mu.Lock()
	saved := make(map[string]savedRunState, len(w.states))
	for key, state := range w.states {
		saved[key] = saveRunState(state)
	}
	data, err := json.Marshal(saved)
	w.mu.Unlock()
	if err != nil || bytes.Equal(data, w.savedStates) {
		return
	}
	if err := writeFileAtomic(path, data); err != nil {
		log.Printf("telegram: save render state failed: %v", err)
		return
	}
	w.savedStates = data
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
