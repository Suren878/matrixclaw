// Package grok is the xAI Grok Voice realtime voice provider.
package grok

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// Spec is Grok Voice's catalog and codec.
var Spec = realtime.ProviderSpec{
	ID:           realtime.ProviderGrok,
	Name:         "Grok Voice",
	Endpoint:     "wss://api.x.ai/v1/realtime",
	DefaultModel: "grok-voice-latest",
	DefaultVoice: "eve",
	KeyEnvs:      []string{"XAI_API_KEY", "GROK_API_KEY"},
	LLMProviders: []string{"xai"},
	Models:       []string{"grok-voice-latest", "grok-voice-think-fast-1.0", "grok-voice-fast-1.0"},
	Voices:       []string{"eve", "ara", "rex", "sal", "leo"},
	Languages: []realtime.Language{
		realtime.AutoLanguage,
		{Code: "en", Name: "English", Aliases: []string{"en-us", "en-gb"}},
		{Code: "ar-EG", Name: "Arabic (Egypt)", Aliases: []string{"ar"}},
		{Code: "ar-SA", Name: "Arabic (Saudi Arabia)"},
		{Code: "ar-AE", Name: "Arabic (United Arab Emirates)"},
		{Code: "bn", Name: "Bengali", Aliases: []string{"bn-bd"}},
		{Code: "zh", Name: "Chinese", Aliases: []string{"zh-cn"}},
		{Code: "fr", Name: "French", Aliases: []string{"fr-fr"}},
		{Code: "de", Name: "German", Aliases: []string{"de-de"}},
		{Code: "hi", Name: "Hindi", Aliases: []string{"hi-in"}},
		{Code: "id", Name: "Indonesian", Aliases: []string{"id-id"}},
		{Code: "it", Name: "Italian", Aliases: []string{"it-it"}},
		{Code: "ja", Name: "Japanese", Aliases: []string{"ja-jp"}},
		{Code: "ko", Name: "Korean", Aliases: []string{"ko-kr"}},
		{Code: "pt-BR", Name: "Portuguese (Brazil)", Aliases: []string{"pt"}},
		{Code: "pt-PT", Name: "Portuguese (Portugal)"},
		{Code: "ru", Name: "Russian", Aliases: []string{"ru-ru"}},
		{Code: "es-MX", Name: "Spanish (Mexico)", Aliases: []string{"es"}},
		{Code: "es-ES", Name: "Spanish (Spain)"},
		{Code: "tr", Name: "Turkish", Aliases: []string{"tr-tr"}},
		{Code: "vi", Name: "Vietnamese", Aliases: []string{"vi-vn"}},
	},
	CheckKey: realtime.BearerKeyCheck("https://api.x.ai/v1/models", "xAI"),
	Dial:     dial,
	NewCodec: func(req realtime.ProviderConnectRequest) realtime.Codec { return &codec{req: req} },
}

func dial(cfg realtime.ProviderConfig, modelID string) (string, http.Header, error) {
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return "", nil, fmt.Errorf("invalid websocket url: %w", err)
	}
	query := parsed.Query()
	if query.Get("model") == "" {
		query.Set("model", modelID)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), http.Header{"Authorization": []string{"Bearer " + cfg.APIKey}}, nil
}

type codec struct {
	req realtime.ProviderConnectRequest
}

func (c *codec) Setup() ([]any, error) {
	return []any{sessionUpdateMessage(c.req)}, nil
}

func (c *codec) SetupDone(msg []byte) (bool, error) {
	var decoded serverMessage
	if err := json.Unmarshal(msg, &decoded); err != nil {
		return false, fmt.Errorf("decode setup response: %w", err)
	}
	switch decoded.Type {
	case "session.updated":
		return true, nil
	case "error":
		if decoded.Error != nil {
			return false, errors.New(textutil.FirstNonEmpty(decoded.Error.Message, decoded.Error.Code, "session update failed"))
		}
		return false, errors.New("session update failed")
	default:
		return false, nil
	}
}

func (c *codec) Encode(input realtime.ProviderInput) ([]any, error) {
	switch input.Type {
	case realtime.ProviderInputAudioAppend:
		if audio := strings.TrimSpace(input.AudioBase64); audio != "" {
			return []any{map[string]any{"type": "input_audio_buffer.append", "audio": audio}}, nil
		}
	case realtime.ProviderInputTextAppend:
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil, nil
		}
		out := []any{map[string]any{
			"type": "conversation.item.create",
			"item": map[string]any{
				"type":    "message",
				"role":    "user",
				"content": []map[string]string{{"type": "input_text", "text": text}},
			},
		}}
		if input.EndOfTurn {
			out = append(out, map[string]any{"type": "response.create"})
		}
		return out, nil
	case realtime.ProviderInputCancel:
		return []any{map[string]any{"type": "response.cancel"}}, nil
	case realtime.ProviderInputToolResult:
		out := []any{}
		for _, response := range input.ToolResponses {
			body, err := json.Marshal(response.Response)
			if err != nil {
				body = []byte(`{"content":"ok"}`)
			}
			out = append(out, map[string]any{
				"type": "conversation.item.create",
				"item": map[string]any{"type": "function_call_output", "call_id": strings.TrimSpace(response.ID), "output": string(body)},
			})
		}
		if len(out) > 0 {
			out = append(out, map[string]any{"type": "response.create"})
		}
		return out, nil
	}
	return nil, nil
}

func (c *codec) Decode(msg []byte) []realtime.ProviderOutput {
	return decodeServerOutputs(msg)
}

func sessionUpdateMessage(req realtime.ProviderConnectRequest) map[string]any {
	transcription := map[string]any{"model": "grok-transcribe"}
	if req.Language != "auto" {
		transcription["language_hint"] = req.Language
	}
	session := map[string]any{
		"voice":        req.VoiceID,
		"instructions": strings.TrimSpace(req.SystemInstruction),
		"turn_detection": map[string]any{
			"type":                "server_vad",
			"threshold":           0.5,
			"prefix_padding_ms":   500,
			"silence_duration_ms": 750,
		},
		"audio": map[string]any{
			"input": map[string]any{
				"format":        map[string]any{"type": "audio/pcm", "rate": realtime.DefaultInputAudioFormat().SampleRateHz},
				"transcription": transcription,
			},
			"output": map[string]any{
				"format": map[string]any{"type": "audio/pcm", "rate": realtime.DefaultOutputAudioFormat().SampleRateHz},
			},
		},
	}
	if declarations := realtime.FunctionDeclarations(req.Tools, true, false); len(declarations) > 0 {
		session["tools"] = declarations
	}
	return map[string]any{"type": "session.update", "session": session}
}
