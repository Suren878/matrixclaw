package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

type Health = core.HealthResponse

func (c *Client) Health(ctx context.Context) (Health, error) {
	var response Health
	if err := c.doJSON(ctx, http.MethodGet, "/v1/health", nil, &response); err != nil {
		return Health{}, err
	}
	return response, nil
}

func (c *Client) ServerStatus(ctx context.Context) (core.ServerStatus, error) {
	var response core.ServerStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/server/status", nil, &response); err != nil {
		return core.ServerStatus{}, err
	}
	return response.Status, nil
}

func (c *Client) RestartDaemon(ctx context.Context) error {
	var response core.OKResponse
	return c.doJSON(ctx, http.MethodPost, "/v1/admin/restart", nil, &response)
}

func (c *Client) RestartDaemonWithNotification(ctx context.Context, notification core.ClientDeliveryTarget) error {
	var response core.OKResponse
	return c.doJSON(ctx, http.MethodPost, "/v1/admin/restart", core.AdminRestartRequest{Notification: &notification}, &response)
}
