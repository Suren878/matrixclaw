package telephony

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// gateway is the telephony gateway's HTTP API.
type gateway struct {
	url   string
	token string
	http  *http.Client
}

func newGateway(url string, token string) *gateway {
	return &gateway{url: strings.TrimRight(url, "/"), token: token, http: &http.Client{Timeout: 20 * time.Second}}
}

// statusError is a gateway answer outside 2xx.
type statusError struct{ Status int }

func (e statusError) Error() string { return fmt.Sprintf("gateway returned HTTP %d", e.Status) }

func (g *gateway) do(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.url+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	res, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return statusError{Status: res.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

// PlaceCall starts an outbound call and returns the gateway's call.
func (g *gateway) PlaceCall(ctx context.Context, request map[string]any) (json.RawMessage, error) {
	var response struct {
		Call json.RawMessage `json:"call"`
	}
	err := g.do(ctx, http.MethodPost, "/v1/calls", request, &response)
	return response.Call, err
}

func (g *gateway) EndCall(ctx context.Context, callID string) error {
	return g.do(ctx, http.MethodDelete, "/v1/calls/"+url.PathEscape(callID), nil, nil)
}

// gatewayHealth is the gateway's own view of its readiness.
type gatewayHealth struct {
	Ready bool   `json:"ready"`
	Error string `json:"error"`
}

func (g *gateway) Health(ctx context.Context) (gatewayHealth, error) {
	var health gatewayHealth
	err := g.do(ctx, http.MethodGet, "/v1/health", nil, &health)
	return health, err
}
