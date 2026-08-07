package openai

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

type serverMessage struct {
	Type       string          `json:"type,omitempty"`
	Delta      string          `json:"delta,omitempty"`
	Text       string          `json:"text,omitempty"`
	Transcript string          `json:"transcript,omitempty"`
	ItemID     string          `json:"item_id,omitempty"`
	Error      *serverError    `json:"error,omitempty"`
	Response   *serverResponse `json:"response,omitempty"`
}

type serverError struct {
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type serverResponse struct {
	Status        string                `json:"status,omitempty"`
	StatusDetails *responseStatusDetail `json:"status_details,omitempty"`
	Output        []responseOutputItem  `json:"output,omitempty"`
}

type responseStatusDetail struct {
	Type   string       `json:"type,omitempty"`
	Reason string       `json:"reason,omitempty"`
	Error  *serverError `json:"error,omitempty"`
}

type responseOutputItem struct {
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type messageDecoder struct {
	pendingInput     map[string]struct{}
	pendingResponses []pendingResponse
}

type pendingResponse struct {
	response *serverResponse
	raw      json.RawMessage
}

func newMessageDecoder() *messageDecoder {
	return &messageDecoder{
		pendingInput: map[string]struct{}{},
	}
}

func (d *messageDecoder) Decode(data []byte) []realtime.ProviderOutput {
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return []realtime.ProviderOutput{{
			Type:  realtime.ProviderOutputError,
			Error: "decode OpenAI Realtime message: " + err.Error(),
		}}
	}
	raw := append(json.RawMessage(nil), data...)
	switch strings.TrimSpace(msg.Type) {
	case "input_audio_buffer.committed":
		if itemID := strings.TrimSpace(msg.ItemID); itemID != "" {
			d.pendingInput[itemID] = struct{}{}
		}
		return nil
	case "conversation.item.input_audio_transcription.delta":
		return nil
	case "conversation.item.input_audio_transcription.completed":
		return d.inputTranscriptCompleted(msg, raw)
	case "conversation.item.input_audio_transcription.failed":
		outputs := []realtime.ProviderOutput{{
			Type:  realtime.ProviderOutputError,
			Error: serverErrorMessage(msg.Error, "OpenAI input transcription failed"),
			Raw:   raw,
		}}
		delete(d.pendingInput, strings.TrimSpace(msg.ItemID))
		return append(outputs, d.flushPendingResponses()...)
	case "response.output_audio.delta":
		if delta := strings.TrimSpace(msg.Delta); delta != "" {
			return []realtime.ProviderOutput{{
				Type:        realtime.ProviderOutputAssistantAudio,
				AudioBase64: delta,
				MIMEType:    "audio/pcm;rate=24000",
				Raw:         raw,
			}}
		}
	case "response.output_audio_transcript.done", "response.output_text.done", "response.text.done":
		if transcript := firstNonEmpty(msg.Transcript, msg.Text); transcript != "" {
			return []realtime.ProviderOutput{{
				Type: realtime.ProviderOutputAssistantTranscript,
				Text: transcript,
				Raw:  raw,
			}}
		}
	case "response.done":
		outputs := responseDoneOutputs(msg.Response, raw)
		if len(d.pendingInput) == 0 || responseWasCancelled(msg.Response) {
			return outputs
		}
		d.pendingResponses = append(d.pendingResponses, pendingResponse{
			response: msg.Response,
			raw:      raw,
		})
		return nil
	case "input_audio_buffer.speech_started":
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputInterrupted, Raw: raw}}
	case "error":
		return []realtime.ProviderOutput{{
			Type:  realtime.ProviderOutputError,
			Error: serverErrorMessage(msg.Error, "OpenAI Realtime error"),
			Raw:   raw,
		}}
	}
	return nil
}

func (d *messageDecoder) inputTranscriptCompleted(msg serverMessage, raw json.RawMessage) []realtime.ProviderOutput {
	transcript := strings.TrimSpace(msg.Transcript)
	itemID := strings.TrimSpace(msg.ItemID)
	delete(d.pendingInput, itemID)
	outputs := make([]realtime.ProviderOutput, 0, 1+len(d.pendingResponses))
	if transcript != "" {
		outputs = append(outputs, realtime.ProviderOutput{
			Type: realtime.ProviderOutputInputTranscript,
			Text: transcript,
			Raw:  raw,
		})
	}
	return append(outputs, d.flushPendingResponses()...)
}

func (d *messageDecoder) flushPendingResponses() []realtime.ProviderOutput {
	if len(d.pendingInput) > 0 || len(d.pendingResponses) == 0 {
		return nil
	}
	pending := d.pendingResponses
	d.pendingResponses = nil
	outputs := make([]realtime.ProviderOutput, 0, len(pending))
	for _, item := range pending {
		outputs = append(outputs, responseDoneOutputs(item.response, item.raw)...)
	}
	return outputs
}

func responseWasCancelled(response *serverResponse) bool {
	if response == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func responseDoneOutputs(response *serverResponse, raw json.RawMessage) []realtime.ProviderOutput {
	if response == nil {
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputTurnComplete, Raw: raw}}
	}
	calls := make([]realtime.ProviderToolCall, 0, len(response.Output))
	for _, item := range response.Output {
		if strings.TrimSpace(item.Type) != "function_call" || strings.TrimSpace(item.Name) == "" {
			continue
		}
		args := json.RawMessage(strings.TrimSpace(item.Arguments))
		if len(args) == 0 || !json.Valid(args) {
			args = json.RawMessage(`{}`)
		}
		calls = append(calls, realtime.ProviderToolCall{
			ID:   strings.TrimSpace(item.CallID),
			Name: strings.TrimSpace(item.Name),
			Args: args,
		})
	}
	if len(calls) > 0 {
		return []realtime.ProviderOutput{{
			Type:      realtime.ProviderOutputToolCall,
			ToolCalls: calls,
			Raw:       raw,
		}}
	}

	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "", "completed":
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputTurnComplete, Raw: raw}}
	case "cancelled", "canceled":
		return nil
	default:
		return []realtime.ProviderOutput{
			{
				Type:  realtime.ProviderOutputError,
				Error: responseErrorMessage(response),
				Raw:   raw,
			},
			{Type: realtime.ProviderOutputTurnComplete, Raw: raw},
		}
	}
}

func responseErrorMessage(response *serverResponse) string {
	if response == nil {
		return "OpenAI Realtime response failed"
	}
	if response.StatusDetails != nil {
		if message := serverErrorMessage(response.StatusDetails.Error, ""); message != "" {
			return message
		}
		if message := firstNonEmpty(response.StatusDetails.Reason, response.StatusDetails.Type); message != "" {
			return "OpenAI Realtime response " + message
		}
	}
	if status := strings.TrimSpace(response.Status); status != "" {
		return "OpenAI Realtime response " + status
	}
	return "OpenAI Realtime response failed"
}

func serverErrorMessage(err *serverError, fallback string) string {
	if err == nil {
		return strings.TrimSpace(fallback)
	}
	return firstNonEmpty(err.Message, err.Code, err.Type, fallback)
}
