package daemonclient

import (
	"context"
	"net/http"

	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
)

func (c *Client) TextToSpeech(ctx context.Context, request voicemodule.TextToSpeechRequest) (voicemodule.TextToSpeechResponse, error) {
	var response voicemodule.TextToSpeechResponse
	if err := c.doJSONWithClient(ctx, http.MethodPost, "/v1/modules/voice/tts", request, &response, c.voiceRuntimeHTTPClient()); err != nil {
		return voicemodule.TextToSpeechResponse{}, err
	}
	return response, nil
}

func (c *Client) SpeechToText(ctx context.Context, request voicemodule.SpeechToTextRequest) (voicemodule.SpeechToTextResponse, error) {
	var response voicemodule.SpeechToTextResponse
	if err := c.doJSONWithClient(ctx, http.MethodPost, "/v1/modules/voice/stt", request, &response, c.voiceRuntimeHTTPClient()); err != nil {
		return voicemodule.SpeechToTextResponse{}, err
	}
	return response, nil
}
