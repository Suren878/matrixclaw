package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strings"

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

func (c *Client) CreateRealtimeVoiceSession(ctx context.Context, request realtime.SessionCreateRequest) (realtime.SessionInfo, error) {
	var response realtime.SessionCreateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/realtime-voice/sessions", request, &response); err != nil {
		return realtime.SessionInfo{}, err
	}
	return response.Session, nil
}

func (c *Client) RealtimeVoiceSession(ctx context.Context, sessionID string) (realtime.SessionInfo, error) {
	var response realtime.SessionResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/realtime-voice/sessions/"+escapedPath(sessionID), nil, &response); err != nil {
		return realtime.SessionInfo{}, err
	}
	return response.Session, nil
}

func (c *Client) CloseRealtimeVoiceSession(ctx context.Context, sessionID string) (realtime.SessionInfo, error) {
	var response realtime.SessionResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/realtime-voice/sessions/"+escapedPath(sessionID), nil, &response); err != nil {
		return realtime.SessionInfo{}, err
	}
	return response.Session, nil
}

func (c *Client) RealtimeVoiceStreamURL(sessionID string) (string, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(base.Scheme) {
	case "https":
		base.Scheme = "wss"
	default:
		base.Scheme = "ws"
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v1/realtime-voice/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/stream"
	base.RawQuery = ""
	return base.String(), nil
}
