package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type matrixclawClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newMatrixclawClient(baseURL string, token string, httpClient *http.Client) *matrixclawClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	return &matrixclawClient{
		baseURL: trimRightSlash(firstNonEmpty(baseURL, defaultMatrixclawURL)),
		token:   strings.TrimSpace(token),
		http:    httpClient,
	}
}

func (c *matrixclawClient) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *matrixclawClient) postJSON(ctx context.Context, path string, payload any, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, payload, out)
}

func (c *matrixclawClient) doJSON(ctx context.Context, method string, path string, payload any, out any) error {
	if c == nil {
		return fmt.Errorf("matrixclaw API client is not configured")
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorize(req)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	responseBody, readErr := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return matrixclawStatusError{StatusCode: res.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	if readErr != nil {
		return readErr
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}

func (c *matrixclawClient) authorize(req *http.Request) {
	if c != nil && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

type matrixclawStatusError struct {
	StatusCode int
	Body       string
}

func (e matrixclawStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}
