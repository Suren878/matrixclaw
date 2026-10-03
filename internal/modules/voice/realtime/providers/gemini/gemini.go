// Package gemini is the Gemini Live realtime voice provider.
package gemini

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

const modelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

// knownLiveModels are listed even when the models API does not mark them as
// bidirectional.
var knownLiveModels = []string{
	"gemini-3.1-flash-live-preview",
	"gemini-2.5-flash-native-audio-preview-12-2025",
	"gemini-2.5-flash-native-audio-preview-09-2025",
}

// Spec is Gemini Live's catalog and codec.
var Spec = realtime.ProviderSpec{
	ID:           realtime.ProviderGemini,
	Name:         "Gemini Live",
	Endpoint:     "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent",
	DefaultVoice: "Puck",
	KeyEnvs:      []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	LLMProviders: []string{"gemini"},
	Voices: []string{
		"Zephyr", "Puck", "Charon", "Kore", "Fenrir", "Leda", "Orus", "Aoede", "Callirrhoe", "Autonoe",
		"Enceladus", "Iapetus", "Umbriel", "Algieba", "Despina", "Erinome", "Algenib", "Rasalgethi", "Laomedeia", "Achernar",
		"Alnilam", "Schedar", "Gacrux", "Pulcherrima", "Achird", "Zubenelgenubi", "Vindemiatrix", "Sadachbia", "Sadaltager", "Sulafat",
	},
	Languages: realtime.RegionalLanguages,
	CheckKey:  listLiveModels,
	Dial:      dial,
	NewCodec:  func(req realtime.ProviderConnectRequest) realtime.Codec { return &codec{req: req} },
}

func dial(cfg realtime.ProviderConfig, _ string) (string, http.Header, error) {
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return "", nil, fmt.Errorf("invalid websocket url: %w", err)
	}
	query := parsed.Query()
	if query.Get("key") == "" {
		query.Set("key", cfg.APIKey)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil, nil
}

type codec struct {
	req realtime.ProviderConnectRequest
}

func (c *codec) Setup() ([]any, error) {
	return []any{setupMessage(c.req)}, nil
}

func (c *codec) SetupDone(msg []byte) (bool, error) {
	var decoded serverMessage
	if err := json.Unmarshal(msg, &decoded); err != nil {
		return false, fmt.Errorf("decode setup response: %w", err)
	}
	if decoded.SetupComplete == nil {
		return false, fmt.Errorf("expected setupComplete, got %s", strings.TrimSpace(string(msg)))
	}
	return true, nil
}

func (c *codec) Encode(input realtime.ProviderInput) ([]any, error) {
	switch input.Type {
	case realtime.ProviderInputAudioAppend:
		return []any{map[string]any{"realtimeInput": map[string]any{"audio": map[string]any{
			"data":     strings.TrimSpace(input.AudioBase64),
			"mimeType": cmp.Or(strings.TrimSpace(input.AudioMIMEType), "audio/pcm;rate=16000"),
		}}}}, nil
	case realtime.ProviderInputAudioEnd:
		return []any{map[string]any{"realtimeInput": map[string]any{"audioStreamEnd": true}}}, nil
	case realtime.ProviderInputTextAppend:
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil, nil
		}
		if !input.EndOfTurn {
			return []any{map[string]any{"realtimeInput": map[string]any{"text": text}}}, nil
		}
		return []any{map[string]any{"clientContent": map[string]any{
			"turns":        []map[string]any{{"role": "user", "parts": []map[string]string{{"text": text}}}},
			"turnComplete": true,
		}}}, nil
	case realtime.ProviderInputToolResult:
		if len(input.ToolResponses) == 0 {
			return nil, nil
		}
		responses := make([]map[string]any, 0, len(input.ToolResponses))
		for _, response := range input.ToolResponses {
			responses = append(responses, map[string]any{
				"id":       strings.TrimSpace(response.ID),
				"name":     strings.TrimSpace(response.Name),
				"response": response.Response,
			})
		}
		return []any{map[string]any{"toolResponse": map[string]any{"functionResponses": responses}}}, nil
	default:
		return nil, nil
	}
}

func (c *codec) Decode(msg []byte) []realtime.ProviderOutput {
	return decodeServerOutputs(msg)
}

func setupMessage(req realtime.ProviderConnectRequest) map[string]any {
	modelID := strings.TrimPrefix(strings.TrimSpace(req.ModelID), "models/")
	generation := map[string]any{
		"responseModalities": []string{"AUDIO"},
		"thinkingConfig":     thinkingConfig(modelID),
	}
	speech := map[string]any{}
	if req.VoiceID != "" {
		speech["voiceConfig"] = map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": req.VoiceID}}
	}
	// Native audio models detect the language themselves and reject a code.
	if req.Language != "auto" && !strings.Contains(strings.ToLower(modelID), "native-audio") {
		speech["languageCode"] = req.Language
	}
	if len(speech) > 0 {
		generation["speechConfig"] = speech
	}
	setup := map[string]any{
		"model":                    "models/" + modelID,
		"generationConfig":         generation,
		"inputAudioTranscription":  map[string]any{},
		"outputAudioTranscription": map[string]any{},
		"realtimeInputConfig": map[string]any{
			"activityHandling": "START_OF_ACTIVITY_INTERRUPTS",
			"turnCoverage":     "TURN_INCLUDES_ONLY_ACTIVITY",
			"automaticActivityDetection": map[string]any{
				"disabled":                 false,
				"startOfSpeechSensitivity": "START_SENSITIVITY_HIGH",
				"prefixPaddingMs":          200,
				"silenceDurationMs":        600,
				"endOfSpeechSensitivity":   "END_SENSITIVITY_HIGH",
			},
		},
	}
	if instruction := strings.TrimSpace(req.SystemInstruction); instruction != "" {
		setup["systemInstruction"] = map[string]any{"parts": []map[string]string{{"text": instruction}}}
	}
	if declarations := realtime.FunctionDeclarations(req.Tools, false, false); len(declarations) > 0 {
		setup["tools"] = []map[string]any{{"functionDeclarations": declarations}}
	}
	return map[string]any{"setup": setup}
}

func thinkingConfig(modelID string) map[string]any {
	modelID = strings.ToLower(modelID)
	if strings.Contains(modelID, "gemini-3") || strings.Contains(modelID, "3.1") {
		return map[string]any{"thinkingLevel": "low"}
	}
	return map[string]any{"thinkingBudget": 0}
}

// listLiveModels lists the key's models that support live sessions.
func listLiveModels(ctx context.Context, key string) ([]string, error) {
	out := []string{}
	pageToken := ""
	for {
		endpoint := modelsURL
		if pageToken != "" {
			endpoint += "?pageToken=" + url.QueryEscape(pageToken)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-goog-api-key", key)
		req.Header.Set("Accept", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		_ = res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, &realtime.KeyError{Status: res.StatusCode, Message: modelListError(res.StatusCode, body)}
		}
		var page struct {
			Models []struct {
				Name    string   `json:"name"`
				Methods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, model := range page.Models {
			id := strings.TrimPrefix(strings.TrimSpace(model.Name), "models/")
			if id != "" && liveModel(id, model.Methods) && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		if pageToken = strings.TrimSpace(page.NextPageToken); pageToken == "" {
			return out, nil
		}
	}
}

func liveModel(id string, methods []string) bool {
	if strings.Contains(strings.ToLower(id), "live-translate") {
		return false
	}
	for _, method := range methods {
		switch strings.ToLower(strings.TrimSpace(method)) {
		case "bidigeneratecontent", "bidi_generate_content":
			return true
		}
	}
	return slices.ContainsFunc(knownLiveModels, func(known string) bool { return strings.EqualFold(known, id) })
}

func modelListError(status int, body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if message := cmp.Or(strings.TrimSpace(payload.Error.Message), strings.TrimSpace(payload.Error.Status)); message != "" {
			return message
		}
	}
	return fmt.Sprintf("models request returned HTTP %d", status)
}
