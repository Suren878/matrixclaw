package openai

import (
	"encoding/base64"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func sessionUpdateMessage(req realtime.ProviderConnectRequest) map[string]any {
	input := map[string]any{
		"format": map[string]any{
			"type": "audio/pcm",
			"rate": 24000,
		},
		"transcription": transcriptionConfig(req.Language),
		"turn_detection": map[string]any{
			"type":                "server_vad",
			"threshold":           0.5,
			"prefix_padding_ms":   300,
			"silence_duration_ms": 500,
			"create_response":     true,
			"interrupt_response":  true,
		},
	}
	session := map[string]any{
		"type":                "realtime",
		"output_modalities":   []string{"audio"},
		"instructions":        strings.TrimSpace(req.SystemInstruction),
		"parallel_tool_calls": false,
		"audio": map[string]any{
			"input": input,
			"output": map[string]any{
				"format": map[string]any{
					"type": "audio/pcm",
					"rate": 24000,
				},
				"voice": req.VoiceID,
			},
		},
	}
	if strings.HasPrefix(strings.ToLower(req.ModelID), "gpt-realtime-2") {
		session["reasoning"] = map[string]any{"effort": "low"}
	}
	if declarations := realtime.FunctionDeclarations(req.Tools, true, true); len(declarations) > 0 {
		session["tools"] = declarations
		session["tool_choice"] = "auto"
	}
	return map[string]any{"type": "session.update", "session": session}
}

func transcriptionConfig(language string) map[string]any {
	config := map[string]any{
		"model": "gpt-live-transcribe",
		"delay": "low",
	}
	if code := transcriptionLanguageCode(language); code != "" {
		config["languages"] = []string{code}
	}
	return config
}

func audioAppendMessage(audio []byte) map[string]any {
	return map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(audio),
	}
}

func textMessage(text string) map[string]any {
	return map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "message",
			"role":    "user",
			"content": []map[string]string{{"type": "input_text", "text": strings.TrimSpace(text)}},
		},
	}
}

func toolResultMessage(callID string, output []byte) map[string]any {
	return map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": strings.TrimSpace(callID),
			"output":  string(output),
		},
	}
}

func responseCreateMessage() map[string]any {
	return map[string]any{"type": "response.create"}
}

func transcriptionLanguageCode(language string) string {
	code := strings.ToLower(language)
	if code == "auto" {
		return ""
	}
	if strings.HasPrefix(code, "zh-") {
		return code
	}
	if base, _, ok := strings.Cut(code, "-"); ok {
		return base
	}
	return code
}
