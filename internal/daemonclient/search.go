package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) Search(ctx context.Context, filter core.SearchFilter) (core.SearchReport, error) {
	values := url.Values{}
	values.Set("q", strings.TrimSpace(filter.Query))
	if strings.TrimSpace(filter.SessionID) != "" {
		values.Set("session_id", strings.TrimSpace(filter.SessionID))
	}
	if filter.Limit > 0 {
		values.Set("limit", strconv.Itoa(filter.Limit))
	}
	var response core.SearchResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/search?"+values.Encode(), nil, &response); err != nil {
		return core.SearchReport{}, err
	}
	return response.Search, nil
}
