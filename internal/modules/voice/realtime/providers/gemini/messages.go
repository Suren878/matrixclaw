package gemini

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

type serverMessage struct {
	SetupComplete           map[string]any           `json:"setupComplete,omitempty"`
	ServerContent           *serverContent           `json:"serverContent,omitempty"`
	ToolCall                *toolCall                `json:"toolCall,omitempty"`
	ToolCallCancellation    *toolCallCancellation    `json:"toolCallCancellation,omitempty"`
	GoAway                  *goAway                  `json:"goAway,omitempty"`
	SessionResumptionUpdate *sessionResumptionUpdate `json:"sessionResumptionUpdate,omitempty"`
	UsageMetadata           json.RawMessage          `json:"usageMetadata,omitempty"`
	Data                    string                   `json:"data,omitempty"`
}

type serverContent struct {
	GenerationComplete  bool           `json:"generationComplete,omitempty"`
	TurnComplete        bool           `json:"turnComplete,omitempty"`
	Interrupted         bool           `json:"interrupted,omitempty"`
	InputTranscription  *transcription `json:"inputTranscription,omitempty"`
	OutputTranscription *transcription `json:"outputTranscription,omitempty"`
	ModelTurn           *content       `json:"modelTurn,omitempty"`
}

type transcription struct {
	Text string `json:"text,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts,omitempty"`
}

type part struct {
	Text         string        `json:"text,omitempty"`
	InlineData   *inlineData   `json:"inlineData,omitempty"`
	FunctionCall *functionCall `json:"functionCall,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

type toolCall struct {
	FunctionCalls []functionCall `json:"functionCalls,omitempty"`
}

type functionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
}

type toolCallCancellation struct {
	IDs []string `json:"ids,omitempty"`
}

type goAway struct {
	TimeLeft json.RawMessage `json:"timeLeft,omitempty"`
}

type sessionResumptionUpdate struct {
	NewHandle string `json:"newHandle,omitempty"`
	Resumable bool   `json:"resumable,omitempty"`
}

func decodeServerOutputs(data []byte) []realtime.ProviderOutput {
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return []realtime.ProviderOutput{{Type: realtime.ProviderOutputError, Error: "decode Gemini Live message: " + err.Error()}}
	}
	raw := append(json.RawMessage(nil), data...)
	outputs := []realtime.ProviderOutput{}
	if text := strings.TrimSpace(msg.Data); text != "" {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputAssistantAudio, AudioBase64: text, Raw: raw})
	}
	if msg.ServerContent != nil {
		outputs = append(outputs, decodeServerContent(msg.ServerContent, raw)...)
	}
	if msg.ToolCall != nil {
		calls := make([]realtime.ProviderToolCall, 0, len(msg.ToolCall.FunctionCalls))
		for _, call := range msg.ToolCall.FunctionCalls {
			name := strings.TrimSpace(call.Name)
			if name == "" {
				continue
			}
			calls = append(calls, realtime.ProviderToolCall{
				ID:   strings.TrimSpace(call.ID),
				Name: name,
				Args: call.Args,
			})
		}
		if len(calls) > 0 {
			outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputToolCall, ToolCalls: calls, Raw: raw})
		}
	}
	if msg.ToolCallCancellation != nil {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputInterrupted, Raw: raw})
	}
	if msg.GoAway != nil {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputGoAway, Raw: raw})
	}
	if msg.SessionResumptionUpdate != nil {
		outputs = append(outputs, realtime.ProviderOutput{
			Type:         realtime.ProviderOutputSessionResumption,
			Resumable:    msg.SessionResumptionUpdate.Resumable,
			ResumeHandle: msg.SessionResumptionUpdate.NewHandle,
			Raw:          raw,
		})
	}
	return outputs
}

func decodeServerContent(content *serverContent, raw json.RawMessage) []realtime.ProviderOutput {
	outputs := []realtime.ProviderOutput{}
	if content.InputTranscription != nil && strings.TrimSpace(content.InputTranscription.Text) != "" {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputInputTranscript, Text: strings.TrimSpace(content.InputTranscription.Text), Raw: raw})
	}
	if content.OutputTranscription != nil && strings.TrimSpace(content.OutputTranscription.Text) != "" {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputAssistantTranscript, Text: strings.TrimSpace(content.OutputTranscription.Text), Raw: raw})
	}
	if content.ModelTurn != nil {
		for _, part := range content.ModelTurn.Parts {
			if strings.TrimSpace(part.Text) != "" {
				outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputAssistantTranscript, Text: strings.TrimSpace(part.Text), Raw: raw})
			}
			if part.InlineData != nil && strings.TrimSpace(part.InlineData.Data) != "" {
				outputs = append(outputs, realtime.ProviderOutput{
					Type:        realtime.ProviderOutputAssistantAudio,
					AudioBase64: strings.TrimSpace(part.InlineData.Data),
					MIMEType:    strings.TrimSpace(part.InlineData.MIMEType),
					Raw:         raw,
				})
			}
			if part.FunctionCall != nil && strings.TrimSpace(part.FunctionCall.Name) != "" {
				outputs = append(outputs, realtime.ProviderOutput{
					Type: realtime.ProviderOutputToolCall,
					ToolCalls: []realtime.ProviderToolCall{{
						ID:   strings.TrimSpace(part.FunctionCall.ID),
						Name: strings.TrimSpace(part.FunctionCall.Name),
						Args: part.FunctionCall.Args,
					}},
					Raw: raw,
				})
			}
		}
	}
	if content.Interrupted {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputInterrupted, Raw: raw})
	}
	if content.TurnComplete {
		outputs = append(outputs, realtime.ProviderOutput{Type: realtime.ProviderOutputTurnComplete, Raw: raw})
	}
	return outputs
}
