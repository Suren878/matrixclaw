package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) ListMemories(ctx context.Context, filter core.MemoryFilter) ([]core.MemoryEntry, error) {
	values := url.Values{}
	if strings.TrimSpace(string(filter.Scope)) != "" {
		values.Set("scope", strings.TrimSpace(string(filter.Scope)))
	}
	if strings.TrimSpace(filter.WorkingDir) != "" {
		values.Set("working_dir", strings.TrimSpace(filter.WorkingDir))
	}
	if filter.Limit > 0 {
		values.Set("limit", strconv.Itoa(filter.Limit))
	}
	path := "/v1/memory"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response core.MemoryResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Memories, nil
}
