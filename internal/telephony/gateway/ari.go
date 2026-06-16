package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type ariClient struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

type ariChannel struct {
	ID        string      `json:"id"`
	Name      string      `json:"name,omitempty"`
	State     string      `json:"state,omitempty"`
	Caller    ariCaller   `json:"caller,omitempty"`
	Connected ariCaller   `json:"connected,omitempty"`
	Dialplan  ariDialplan `json:"dialplan,omitempty"`
}

type ariCaller struct {
	Name   string `json:"name,omitempty"`
	Number string `json:"number,omitempty"`
}

type ariDialplan struct {
	Context  string `json:"context,omitempty"`
	Exten    string `json:"exten,omitempty"`
	Priority int    `json:"priority,omitempty"`
	AppName  string `json:"app_name,omitempty"`
	AppData  string `json:"app_data,omitempty"`
}

type ariEvent struct {
	Type    string      `json:"type"`
	Channel *ariChannel `json:"channel,omitempty"`
	Args    []string    `json:"args,omitempty"`
}

type ariBridge struct {
	ID         string   `json:"id"`
	Name       string   `json:"name,omitempty"`
	BridgeType string   `json:"bridge_type,omitempty"`
	Channels   []string `json:"channels,omitempty"`
}

type originateRequest struct {
	ChannelID string
	Endpoint  string
	App       string
	AppArgs   string
	CallerID  string
	Timeout   int
	Formats   string
}

type externalMediaRequest struct {
	ChannelID    string
	App          string
	ExternalHost string
	Format       string
	Data         string
}

type snoopRequest struct {
	ChannelID string
	SnoopID   string
	App       string
	AppArgs   string
	Spy       string
	Whisper   string
}

type ariEvents struct {
	conn *websocket.Conn
}

func newARIClient(baseURL string, user string, password string) *ariClient {
	return &ariClient{
		baseURL:  trimRightSlash(firstNonEmpty(baseURL, defaultARIURL)),
		user:     strings.TrimSpace(user),
		password: strings.TrimSpace(password),
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *ariClient) probe(ctx context.Context) error {
	var payload map[string]any
	return c.do(ctx, http.MethodGet, "/asterisk/info", nil, nil, &payload)
}

func (c *ariClient) originate(ctx context.Context, req originateRequest) error {
	query := url.Values{}
	query.Set("endpoint", req.Endpoint)
	query.Set("app", req.App)
	if req.AppArgs != "" {
		query.Set("appArgs", req.AppArgs)
	}
	if req.CallerID != "" {
		query.Set("callerId", req.CallerID)
	}
	if req.Timeout > 0 {
		query.Set("timeout", strconv.Itoa(req.Timeout))
	}
	if req.Formats != "" {
		query.Set("formats", req.Formats)
	}
	path := "/channels"
	if req.ChannelID != "" {
		path += "/" + url.PathEscape(req.ChannelID)
	}
	return c.do(ctx, http.MethodPost, path, query, nil, nil)
}

func (c *ariClient) createBridge(ctx context.Context, bridgeID string) error {
	query := url.Values{}
	query.Set("type", "mixing,dtmf_events")
	query.Set("name", bridgeID)
	return c.do(ctx, http.MethodPost, "/bridges/"+url.PathEscape(bridgeID), query, nil, nil)
}

func (c *ariClient) bridges(ctx context.Context) ([]ariBridge, error) {
	var out []ariBridge
	err := c.do(ctx, http.MethodGet, "/bridges", nil, nil, &out)
	return out, err
}

func (c *ariClient) destroyBridge(ctx context.Context, bridgeID string) error {
	if strings.TrimSpace(bridgeID) == "" {
		return nil
	}
	return c.do(ctx, http.MethodDelete, "/bridges/"+url.PathEscape(bridgeID), nil, nil, nil)
}

func (c *ariClient) addChannel(ctx context.Context, bridgeID string, channelID string) error {
	query := url.Values{}
	query.Set("channel", channelID)
	return c.do(ctx, http.MethodPost, "/bridges/"+url.PathEscape(bridgeID)+"/addChannel", query, nil, nil)
}

func (c *ariClient) addChannelWithRetry(ctx context.Context, bridgeID string, channelID string) error {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		if err := c.addChannel(ctx, bridgeID, channelID); err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			select {
			case <-time.After(time.Duration(100+attempt*75) * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (c *ariClient) hangup(ctx context.Context, channelID string) error {
	if strings.TrimSpace(channelID) == "" {
		return nil
	}
	err := c.do(ctx, http.MethodDelete, "/channels/"+url.PathEscape(channelID), nil, nil, nil)
	if isARIStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (c *ariClient) channel(ctx context.Context, channelID string) (ariChannel, error) {
	var out ariChannel
	if strings.TrimSpace(channelID) == "" {
		return out, errors.New("channel id is required")
	}
	err := c.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(channelID), nil, nil, &out)
	return out, err
}

func (c *ariClient) channels(ctx context.Context) ([]ariChannel, error) {
	var out []ariChannel
	err := c.do(ctx, http.MethodGet, "/channels", nil, nil, &out)
	return out, err
}

func (c *ariClient) waitChannelEnd(ctx context.Context, channelID string) error {
	if strings.TrimSpace(channelID) == "" {
		return errors.New("channel id is required")
	}
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	consecutiveErrors := 0
	for {
		_, err := c.channel(ctx, channelID)
		switch {
		case err == nil:
			consecutiveErrors = 0
		case isARIStatus(err, http.StatusNotFound):
			return nil
		default:
			consecutiveErrors++
			if consecutiveErrors >= 5 {
				return err
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *ariClient) answer(ctx context.Context, channelID string) error {
	if strings.TrimSpace(channelID) == "" {
		return nil
	}
	err := c.do(ctx, http.MethodPost, "/channels/"+url.PathEscape(channelID)+"/answer", nil, nil, nil)
	if isARIStatus(err, http.StatusNotFound) || isARIStatus(err, http.StatusConflict) {
		return nil
	}
	return err
}

func (c *ariClient) stopSilence(ctx context.Context, channelID string) error {
	if strings.TrimSpace(channelID) == "" {
		return nil
	}
	err := c.do(ctx, http.MethodDelete, "/channels/"+url.PathEscape(channelID)+"/silence", nil, nil, nil)
	if isARIStatus(err, http.StatusNotFound) || isARIStatus(err, http.StatusConflict) {
		return nil
	}
	return err
}

func (c *ariClient) snoop(ctx context.Context, req snoopRequest) (ariChannel, error) {
	var out ariChannel
	target := strings.TrimSpace(req.ChannelID)
	if target == "" {
		return out, errors.New("channel id is required")
	}
	query := url.Values{}
	query.Set("app", req.App)
	query.Set("spy", firstNonEmpty(req.Spy, "none"))
	query.Set("whisper", firstNonEmpty(req.Whisper, "none"))
	if req.AppArgs != "" {
		query.Set("appArgs", req.AppArgs)
	}
	path := "/channels/" + url.PathEscape(target) + "/snoop"
	if strings.TrimSpace(req.SnoopID) != "" {
		path += "/" + url.PathEscape(strings.TrimSpace(req.SnoopID))
	}
	err := c.do(ctx, http.MethodPost, path, query, nil, &out)
	return out, err
}

func (c *ariClient) externalMedia(ctx context.Context, req externalMediaRequest) (ariChannel, error) {
	query := url.Values{}
	query.Set("app", req.App)
	query.Set("external_host", req.ExternalHost)
	query.Set("format", firstNonEmpty(req.Format, "alaw"))
	query.Set("encapsulation", "rtp")
	query.Set("transport", "udp")
	query.Set("connection_type", "client")
	query.Set("direction", "both")
	if req.ChannelID != "" {
		query.Set("channelId", req.ChannelID)
	}
	if req.Data != "" {
		query.Set("data", req.Data)
	}
	var out ariChannel
	err := c.do(ctx, http.MethodPost, "/channels/externalMedia", query, nil, &out)
	return out, err
}

func (c *ariClient) rtpAddress(ctx context.Context, channelID string) (*net.UDPAddr, error) {
	host, err := c.channelVariable(ctx, channelID, "UNICASTRTP_LOCAL_ADDRESS")
	if err != nil {
		return nil, err
	}
	portText, err := c.channelVariable(ctx, channelID, "UNICASTRTP_LOCAL_PORT")
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("invalid RTP port %q", portText)
	}
	return net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
}

func (c *ariClient) channelVariable(ctx context.Context, channelID string, variable string) (string, error) {
	query := url.Values{}
	query.Set("variable", variable)
	var payload struct {
		Value string `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(channelID)+"/variable", query, nil, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Value), nil
}

func (c *ariClient) events(ctx context.Context, app string) (*ariEvents, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported ARI URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/events"
	query := parsed.Query()
	query.Set("app", app)
	query.Set("subscribeAll", "true")
	parsed.RawQuery = query.Encode()
	headers := http.Header{}
	if c.user != "" || c.password != "" {
		headers.Set("Authorization", "Basic "+basicAuth(c.user, c.password))
	}
	conn, _, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	return &ariEvents{conn: conn}, nil
}

func (e *ariEvents) Close() {
	if e != nil && e.conn != nil {
		_ = e.conn.Close(websocket.StatusNormalClosure, "done")
	}
}

func (e *ariEvents) ping(ctx context.Context) error {
	if e == nil || e.conn == nil {
		return errors.New("ARI events websocket is not connected")
	}
	return e.conn.Ping(ctx)
}

func (e *ariEvents) read(ctx context.Context) (ariEvent, error) {
	if e == nil || e.conn == nil {
		return ariEvent{}, errors.New("ARI events websocket is not connected")
	}
	for {
		messageType, data, err := e.conn.Read(ctx)
		if err != nil {
			return ariEvent{}, err
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		var event ariEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return ariEvent{}, err
		}
		return event, nil
	}
}

func (c *ariClient) do(ctx context.Context, method string, path string, query url.Values, body any, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.user != "" || c.password != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	responseBody, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ariStatusError{StatusCode: res.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}

type ariStatusError struct {
	StatusCode int
	Body       string
}

func (e ariStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("ARI HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("ARI HTTP %d: %s", e.StatusCode, e.Body)
}

func isARIStatus(err error, status int) bool {
	var statusErr ariStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == status
}
