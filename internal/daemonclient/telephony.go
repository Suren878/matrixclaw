package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) TelephonyModule(ctx context.Context) (setup.TelephonyModuleDescriptor, error) {
	var response setup.TelephonyModuleResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/telephony", nil, &response); err != nil {
		return setup.TelephonyModuleDescriptor{}, err
	}
	return response.Module, nil
}

func (c *Client) UpdateTelephonyModule(ctx context.Context, update setup.TelephonyModuleUpdate) (setup.TelephonyModuleDescriptor, error) {
	var response setup.TelephonyModuleResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v1/modules/telephony", update, &response); err != nil {
		return setup.TelephonyModuleDescriptor{}, err
	}
	return response.Module, nil
}
