// Package openai is the OpenAI Realtime voice provider.
package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

const outputSampleRate = 24000

// Spec is OpenAI Realtime's catalog and codec.
var Spec = realtime.ProviderSpec{
	ID:           realtime.ProviderOpenAI,
	Name:         "OpenAI Realtime",
	Endpoint:     "wss://api.openai.com/v1/realtime",
	DefaultModel: "gpt-realtime-2.1",
	DefaultVoice: "marin",
	KeyEnvs:      []string{"OPENAI_API_KEY"},
	LLMProviders: []string{"openai"},
	Models:       []string{"gpt-realtime-2.1", "gpt-realtime-2.1-mini"},
	Voices:       []string{"alloy", "ash", "ballad", "coral", "echo", "sage", "shimmer", "verse", "marin", "cedar"},
	Languages:    realtime.RegionalLanguages,
	CheckKey:     realtime.BearerKeyCheck("https://api.openai.com/v1/models", "OpenAI"),
	Dial: func(cfg realtime.ProviderConfig, modelID string) (string, http.Header, error) {
		endpoint, err := realtimeURL(cfg.Endpoint, modelID)
		return endpoint, http.Header{"Authorization": []string{"Bearer " + cfg.APIKey}}, err
	},
	NewCodec: func(req realtime.ProviderConnectRequest) realtime.Codec {
		// OpenAI takes 24 kHz input; clients send 16 kHz.
		return &codec{req: req, decoder: newMessageDecoder(), resampler: newPCM16Resampler(req.InputAudio.SampleRateHz, outputSampleRate)}
	},
}

func realtimeURL(raw string, modelID string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid websocket url: %w", err)
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", errors.New("websocket url must use ws or wss")
	}
	query := parsed.Query()
	query.Set("model", modelID)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

type codec struct {
	req       realtime.ProviderConnectRequest
	decoder   *messageDecoder
	resampler *pcm16Resampler
}

func (c *codec) Setup() ([]any, error) {
	return []any{sessionUpdateMessage(c.req)}, nil
}

func (c *codec) SetupDone(msg []byte) (bool, error) {
	var decoded serverMessage
	if err := json.Unmarshal(msg, &decoded); err != nil {
		return false, fmt.Errorf("decode setup response: %w", err)
	}
	switch strings.TrimSpace(decoded.Type) {
	case "session.updated":
		return true, nil
	case "error":
		return false, errors.New(serverErrorMessage(decoded.Error, "session update failed"))
	default:
		return false, nil
	}
}

func (c *codec) Encode(input realtime.ProviderInput) ([]any, error) {
	switch input.Type {
	case realtime.ProviderInputAudioAppend:
		encoded := strings.TrimSpace(input.AudioBase64)
		if encoded == "" {
			return nil, nil
		}
		audio, err := decodeBase64Audio(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode input audio: %w", err)
		}
		converted, err := c.resampler.Convert(audio)
		if err != nil {
			return nil, fmt.Errorf("resample input audio: %w", err)
		}
		if len(converted) == 0 {
			return nil, nil
		}
		return []any{audioAppendMessage(converted)}, nil
	case realtime.ProviderInputAudioEnd:
		if audio := c.resampler.Flush(); len(audio) > 0 {
			return []any{audioAppendMessage(audio)}, nil
		}
	case realtime.ProviderInputTextAppend:
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil, nil
		}
		out := []any{textMessage(text)}
		if input.EndOfTurn {
			out = append(out, responseCreateMessage())
		}
		return out, nil
	case realtime.ProviderInputCancel:
		return []any{map[string]any{"type": "response.cancel"}}, nil
	case realtime.ProviderInputToolResult:
		out := []any{}
		for _, response := range input.ToolResponses {
			body, err := json.Marshal(response.Response)
			if err != nil {
				body = []byte(`{"error":"could not encode tool result"}`)
			}
			out = append(out, toolResultMessage(response.ID, body))
		}
		if len(out) > 0 {
			out = append(out, responseCreateMessage())
		}
		return out, nil
	}
	return nil, nil
}

func (c *codec) Decode(msg []byte) []realtime.ProviderOutput {
	return c.decoder.Decode(msg)
}
