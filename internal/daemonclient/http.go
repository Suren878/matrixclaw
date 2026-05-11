package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) doJSON(ctx context.Context, method string, path string, body any, out any) error {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = defaultHTTPClient
	}
	var bodyReader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(payload)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorize(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeAPIError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) authorize(req *http.Request) {
	if c == nil || req == nil {
		return
	}
	token := strings.TrimSpace(c.APIToken)
	if token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

func decodeAPIError(resp *http.Response) error {
	var payload core.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload.Error == "" {
		payload.Error = resp.Status
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    strings.TrimSpace(payload.Error),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
