package grok

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

type serverMessage struct {
	Type         string          `json:"type,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Text         string          `json:"text,omitempty"`
	Transcript   string          `json:"transcript,omitempty"`
	Name         string          `json:"name,omitempty"`
	CallID       string          `json:"call_id,omitempty"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	Error        *serverError    `json:"error,omitempty"`
	Conversation *conversation   `json:"conversation,omitempty"`
}

type serverError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type conversation struct {
	ID string `json:"id,omitempty"`
}

func decodeServerOutputs(data []byte) []realtime.ProviderOutput {
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputError, Error: "decode Grok Voice message: " + err.Error()}}
	}
	raw := append(json.RawMessage(nil), data...)
	switch strings.TrimSpace(msg.Type) {
	case "conversation.item.input_audio_transcription.updated":
		return nil
	case "conversation.item.input_audio_transcription.completed":
		if text := firstNonEmpty(msg.Transcript, msg.Text, msg.Delta); text != "" {
			return []realtime.ProviderOutput{{Type: realtime.ProviderOutputInputTranscript, Text: text, Raw: raw}}
		}
	case "response.output_audio.delta":
		if text := strings.TrimSpace(msg.Delta); text != "" {
			return []realtime.ProviderOutput{{Type: realtime.ProviderOutputAssistantAudio, AudioBase64: text, Raw: raw}}
		}
	case "response.output_audio_transcript.delta", "response.text.delta", "response.output_text.delta":
		if text := firstNonEmpty(msg.Delta, msg.Text, msg.Transcript); text != "" {
			return []realtime.ProviderOutput{{Type: realtime.ProviderOutputAssistantTranscript, Text: text, Raw: raw}}
		}
	case "response.output_audio_transcript.done":
		return nil
	case "response.function_call_arguments.done":
		name := strings.TrimSpace(msg.Name)
		if name == "" {
			return nil
		}
		args := msg.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		return []realtime.ProviderOutput{{
			Type: realtime.ProviderOutputToolCall,
			ToolCalls: []realtime.ProviderToolCall{{
				ID:   strings.TrimSpace(msg.CallID),
				Name: name,
				Args: args,
			}},
			Raw: raw,
		}}
	case "response.done":
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputTurnComplete, Raw: raw}}
	case "input_audio_buffer.speech_started":
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputInterrupted, Raw: raw}}
	case "error":
		if msg.Error != nil {
			return []realtime.ProviderOutput{{Type: realtime.ProviderOutputError, Error: firstNonEmpty(msg.Error.Message, msg.Error.Code, "Grok Voice error"), Raw: raw}}
		}
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputError, Error: "Grok Voice error", Raw: raw}}
	}
	return nil
}
