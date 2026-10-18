package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/hooks"
)

func (c *Client) SessionHooks(ctx context.Context, sessionID string) (hooks.Listing, error) {
	var listing hooks.Listing
	err := c.doJSON(ctx, http.MethodGet, "/v1/sessions/"+escapedPath(sessionID)+"/hooks", nil, &listing)
	return listing, err
}

func (c *Client) TrustSessionHooks(ctx context.Context, sessionID string, sum string) (hooks.Listing, error) {
	var listing hooks.Listing
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/"+escapedPath(sessionID)+"/hooks/trust", core.TrustHooksRequest{SHA256: sum}, &listing)
	return listing, err
}
