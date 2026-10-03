package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const TextToSpeechToolID = "text_to_speech"

type textToSpeechTool struct {
	module *Module
}

func (t *textToSpeechTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          TextToSpeechToolID,
		Description: "Generate spoken audio through Matrixclaw text-to-speech for the current client. Use this when the user asks for voice, spoken, audio, or TTS output.",
		Effect:      tools.EffectMutation,
		Namespace:   "module.voice",
		Category:    tools.CategoryAutomation,
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
	var input struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid text_to_speech arguments.", Status: tools.ResultStatusError}, nil
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return tools.Result{Content: "Text is required.", Status: tools.ResultStatusError}, nil
	}
	response, err := t.module.TextToSpeech(ctx, TextToSpeechRequest{Text: text})
	if err != nil {
		if errors.Is(err, ErrModuleDisabled) {
			return tools.Result{Content: "Text to speech is disabled.", Status: tools.ResultStatusError}, nil
		}
		return tools.Result{Content: fmt.Sprintf("Text to speech failed: %s", err), Status: tools.ResultStatusError}, nil
	}
	return tools.Result{
		Content:  "Speech audio generated.",
		Metadata: response,
		MIMEType: response.MIMEType,
		Status:   tools.ResultStatusSuccess,
	}, nil
}
