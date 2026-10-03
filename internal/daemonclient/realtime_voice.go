package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) RealtimeVoiceModule(ctx context.Context) (realtime.ModuleDescriptor, error) {
	var response realtime.ModuleResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/voice/realtime_voice", nil, &response); err != nil {
		return realtime.ModuleDescriptor{}, err
	}
	return response.Module, nil
}

func (c *Client) UpdateRealtimeVoiceModule(ctx context.Context, update setup.VoiceModuleUpdate) (realtime.ModuleDescriptor, error) {
	var response realtime.ModuleResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v1/modules/voice/realtime_voice", update, &response); err != nil {
		return realtime.ModuleDescriptor{}, err
	}
	return response.Module, nil
}
