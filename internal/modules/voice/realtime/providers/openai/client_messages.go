package openai

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func sessionUpdateMessage(modelID string, voiceID string, language string, instructions string, tools []realtime.ToolDeclaration) map[string]any {
	input := map[string]any{
		"format": map[string]any{
			"type": "audio/pcm",
			"rate": 24000,
		},
		"transcription": transcriptionConfig(language),
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
		"instructions":        strings.TrimSpace(instructions),
		"parallel_tool_calls": false,
		"audio": map[string]any{
			"input": input,
			"output": map[string]any{
				"format": map[string]any{
					"type": "audio/pcm",
					"rate": 24000,
				},
				"voice": firstNonEmpty(voiceID, defaultVoice),
			},
		},
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelID)), "gpt-realtime-2") {
		session["reasoning"] = map[string]any{"effort": "low"}
	}
	if declarations := functionDeclarations(tools); len(declarations) > 0 {
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

func functionDeclarations(tools []realtime.ToolDeclaration) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		declaration := map[string]any{
			"type":        "function",
			"name":        name,
			"description": strings.TrimSpace(tool.Description),
		}
		if len(tool.Parameters) > 0 {
			var parameters any
			if err := json.Unmarshal(tool.Parameters, &parameters); err == nil {
				declaration["parameters"] = sanitizeFunctionSchema(parameters)
			}
		}
		out = append(out, declaration)
		if len(out) >= 128 {
			break
		}
	}
	return out
}

func sanitizeFunctionSchema(value any) any {
	switch item := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, child := range item {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "properties":
				if properties, ok := child.(map[string]any); ok {
					clean := make(map[string]any, len(properties))
					for name, schema := range properties {
						if name = strings.TrimSpace(name); name != "" {
							clean[name] = sanitizeFunctionSchema(schema)
						}
					}
					out[key] = clean
				}
			case "items":
				out[key] = sanitizeFunctionSchema(child)
			case "enum":
				if values, ok := child.([]any); ok {
					out[key] = sanitizeFunctionSchema(values)
				}
			case "type", "format", "description", "nullable", "required", "minimum", "maximum", "minitems", "maxitems", "minlength", "maxlength", "additionalproperties":
				out[key] = sanitizeFunctionSchema(child)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(item))
		for _, child := range item {
			if text, ok := child.(string); ok && strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, sanitizeFunctionSchema(child))
		}
		return out
	default:
		return value
	}
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

func combinedSystemInstruction(base string, session string, language string) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{base, session, languageSystemInstruction(language)} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func languageSystemInstruction(language string) string {
	code := normalizeLanguageCode(language)
	if code == "auto" {
		return "Realtime voice language policy:\n" +
			"- Detect the human's language from their speech and answer in that language unless they explicitly ask to switch.\n" +
			"- Keep the established conversation language when speech is ambiguous."
	}
	return "Realtime voice language policy:\n" +
		"- Speak in " + code + " unless the human explicitly asks to switch languages.\n" +
		"- Keep pronunciation and accent natural for " + code + "."
}

func normalizeLanguageCode(language string) string {
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	default:
		return value
	}
}

func transcriptionLanguageCode(language string) string {
	code := normalizeLanguageCode(language)
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
