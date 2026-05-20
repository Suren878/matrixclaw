package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const TextToSpeechToolID = "text_to_speech"

type textToSpeechTool struct {
	service *Service
}

type textToSpeechToolMetadata struct {
	Type          string `json:"type"`
	Delivery      string `json:"delivery"`
	ContentBase64 string `json:"content_base64"`
	MIMEType      string `json:"mime_type"`
	FileName      string `json:"file_name"`
}

func NewTextToSpeechTool(setupService setupLoader) tools.Executor {
	return &textToSpeechTool{service: NewService(setupService)}
}

func (t *textToSpeechTool) Spec() tools.Spec {
	return tools.Spec{
		ID:           TextToSpeechToolID,
		Name:         "Text to Speech",
		Description:  "Generate spoken audio through Matrixclaw text-to-speech for the current client. Use this when the user asks for voice, spoken, audio, or TTS output.",
		Risk:         tools.RiskSafe,
		Effect:       tools.EffectMutation,
		ApprovalMode: tools.ApprovalNever,
		Namespace:    "module.voice",
		Category:     tools.CategoryAutomation,
		Profiles:     []tools.Profile{tools.ProfileAutomation, tools.ProfileCoding},
		OutputKind:   tools.OutputAudio,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "Text to synthesize into speech."}
  },
  "required": ["text"],
  "additionalProperties": false
}`),
	}
}

func (t *textToSpeechTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.service == nil {
		return tools.Result{Content: "Text to speech is not configured.", IsError: true, Status: tools.ResultStatusError}, nil
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid text_to_speech arguments.", IsError: true, Status: tools.ResultStatusError}, nil
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return tools.Result{Content: "Text is required.", IsError: true, Status: tools.ResultStatusError}, nil
	}
	response, err := t.service.TextToSpeech(ctx, TextToSpeechRequest{Text: text})
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Text to speech failed: %s", err), IsError: true, Status: tools.ResultStatusError}, nil
	}
	return tools.Result{
		Content: "Speech audio generated.",
		Metadata: textToSpeechToolMetadata{
			Type:          "matrixclaw.tts_audio",
			Delivery:      "voice",
			ContentBase64: response.ContentBase64,
			MIMEType:      response.MIMEType,
			FileName:      response.FileName,
		},
		MIMEType: response.MIMEType,
		Status:   tools.ResultStatusSuccess,
	}, nil
}
