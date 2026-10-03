package daemonclient

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

const defaultJSONTimeout = 10 * time.Second
const defaultCompactJSONTimeout = 2 * time.Minute
const defaultVoiceRuntimeTimeout = 30 * time.Minute

var defaultHTTPClient = &http.Client{Timeout: defaultJSONTimeout}
var defaultCompactHTTPClient = &http.Client{Timeout: defaultCompactJSONTimeout}
var defaultVoiceRuntimeHTTPClient = &http.Client{Timeout: defaultVoiceRuntimeTimeout}

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
	BaseURL      string
	ClientName   string
	ExternalKey  string
	APIToken     string
	Capabilities core.ClientCapabilities
	// Role is who the client acts for, sent in core.RoleHeader; empty acts as
	// the owner. Set it from a verified identity, never from message text.
	Role       core.Role
	HTTPClient *http.Client
	// EventHTTPClient is intentionally separate from HTTPClient because SSE
	// subscriptions must not inherit the short JSON request timeout.
	EventHTTPClient *http.Client
}

// BaseURL turns a daemon listen address such as 127.0.0.1:8080 into its URL.
func BaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + addr
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

func (c *Client) WithCapabilities(capabilities core.ClientCapabilities) *Client {
	if c == nil {
		return c
	}
	c.Capabilities = capabilities
	return c
}
