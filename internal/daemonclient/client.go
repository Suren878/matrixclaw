package daemonclient

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultJSONTimeout = 10 * time.Second
const defaultCompactJSONTimeout = 2 * time.Minute

var defaultHTTPClient = &http.Client{Timeout: defaultJSONTimeout}
var defaultCompactHTTPClient = &http.Client{Timeout: defaultCompactJSONTimeout}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return "daemon api error"
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("daemon api: %d", e.StatusCode)
	}
	return "daemon api: " + e.Message
}

func IsAPIStatus(err error, statusCode int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == statusCode
}

type Client struct {
	BaseURL     string
	ClientName  string
	ExternalKey string
	APIToken    string
	HTTPClient  *http.Client
	// EventHTTPClient is intentionally separate from HTTPClient because SSE
	// subscriptions must not inherit the short JSON request timeout.
	EventHTTPClient *http.Client
}

func New(baseURL string, clientName string, externalKey string) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		ClientName:  strings.TrimSpace(clientName),
		ExternalKey: strings.TrimSpace(externalKey),
		HTTPClient:  defaultHTTPClient,
	}
}

func (c *Client) WithAPIToken(token string) *Client {
	if c == nil {
		return c
	}
	c.APIToken = strings.TrimSpace(token)
	return c
}
