package daemonclient

import (
	"context"
	"net/http"

	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) VoiceModules(ctx context.Context) ([]setup.VoiceModuleDescriptor, error) {
	var response setup.VoiceModulesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/voice", nil, &response); err != nil {
		return nil, err
	}
	return response.Modules, nil
}

func (c *Client) UpdateVoiceModule(ctx context.Context, moduleID string, update setup.VoiceModuleUpdate) ([]setup.VoiceModuleDescriptor, error) {
	var response setup.VoiceModulesResponse
	path := "/v1/modules/voice/" + escapedPath(moduleID)
	if err := c.doJSON(ctx, http.MethodPatch, path, update, &response); err != nil {
		return nil, err
	}
	return response.Modules, nil
}

func (c *Client) VoiceProviderAction(ctx context.Context, moduleID string, providerID string, request setup.VoiceProviderActionRequest) (setup.VoiceProviderOption, error) {
	var response setup.VoiceProviderActionResponse
	path := "/v1/modules/voice/" + escapedPath(moduleID) + "/providers/" + escapedPath(providerID) + "/action"
	if err := c.doJSONWithClient(ctx, http.MethodPost, path, request, &response, c.voiceRuntimeHTTPClient()); err != nil {
		return setup.VoiceProviderOption{}, err
	}
	return response.Provider, nil
}

func (c *Client) TextToSpeech(ctx context.Context, request voicemodule.TextToSpeechRequest) (voicemodule.TextToSpeechResponse, error) {
	var response voicemodule.TextToSpeechResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/voice/tts", request, &response); err != nil {
		return voicemodule.TextToSpeechResponse{}, err
	}
	return response, nil
}

func (c *Client) SpeechToText(ctx context.Context, request voicemodule.SpeechToTextRequest) (voicemodule.SpeechToTextResponse, error) {
	var response voicemodule.SpeechToTextResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/voice/stt", request, &response); err != nil {
		return voicemodule.SpeechToTextResponse{}, err
	}
	return response, nil
}
