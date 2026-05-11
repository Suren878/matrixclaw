package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) ListClientDeliveries(ctx context.Context, filter core.ClientDeliveryFilter) ([]core.ClientDelivery, error) {
	values := url.Values{}
	client := firstNonEmpty(filter.Client, c.ClientName)
	externalKey := firstNonEmpty(filter.ExternalKey, c.ExternalKey)
	if strings.TrimSpace(client) != "" {
		values.Set("client", strings.TrimSpace(client))
	}
	if strings.TrimSpace(externalKey) != "" {
		values.Set("external_key", strings.TrimSpace(externalKey))
	}
	if strings.TrimSpace(filter.SessionID) != "" {
		values.Set("session_id", strings.TrimSpace(filter.SessionID))
	}
	if strings.TrimSpace(filter.RunID) != "" {
		values.Set("run_id", strings.TrimSpace(filter.RunID))
	}
	if strings.TrimSpace(filter.TaskID) != "" {
		values.Set("task_id", strings.TrimSpace(filter.TaskID))
	}
	if strings.TrimSpace(filter.Type) != "" {
		values.Set("type", strings.TrimSpace(filter.Type))
	}
	if filter.Status != "" {
		values.Set("status", string(filter.Status))
	}
	if !filter.CreatedAfter.IsZero() {
		values.Set("created_after", filter.CreatedAfter.UTC().Format(time.RFC3339Nano))
	}
	if filter.Limit > 0 {
		values.Set("limit", strconv.Itoa(filter.Limit))
	}
	var response core.ClientDeliveriesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/client-deliveries?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	return response.Deliveries, nil
}

func (c *Client) AcknowledgeClientDelivery(ctx context.Context, deliveryID string) error {
	path := "/v1/client-deliveries/" + escapedPath(deliveryID) + "/ack"
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}
