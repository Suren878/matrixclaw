package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"nhooyr.io/websocket"
)

type realtimeClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type realtimeConn struct {
	Session realtime.SessionInfo
	conn    *websocket.Conn
	writeMu sync.Mutex
}

type realtimeConnectRequest struct {
	Client            string
	ExternalKey       string
	SessionID         string
	SystemInstruction string
}

func newRealtimeClient(baseURL string, token string) *realtimeClient {
	return &realtimeClient{
		baseURL: trimRightSlash(firstNonEmpty(baseURL, defaultMatrixclawURL)),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *realtimeClient) Connect(ctx context.Context, input realtimeConnectRequest) (*realtimeConn, error) {
	create := realtime.SessionCreateRequest{
		Client:            firstNonEmpty(input.Client, "telephony"),
		ExternalKey:       strings.TrimSpace(input.ExternalKey),
		SessionID:         strings.TrimSpace(input.SessionID),
		SystemInstruction: strings.TrimSpace(input.SystemInstruction),
		InputAudio:        realtime.DefaultInputAudioFormat(),
		OutputAudio:       realtime.DefaultOutputAudioFormat(),
		PersistMode:       realtime.PersistModeTurnsAndSummary,
	}
	payload, err := json.Marshal(create)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/realtime-voice/sessions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("create realtime session returned HTTP %d", res.StatusCode)
	}
	var created realtime.SessionCreateResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		return nil, err
	}
	wsURL, err := c.streamURL(created.Session.ID)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	if c.token != "" {
		headers.Set("Authorization", "Bearer "+c.token)
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	out := &realtimeConn{Session: created.Session, conn: conn}
	if err := out.waitReady(ctx); err != nil {
		_ = out.Close(context.Background())
		return nil, err
	}
	return out, nil
}

func (c *realtimeClient) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *realtimeClient) streamURL(sessionID string) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported MatrixClaw URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/realtime-voice/sessions/" + url.PathEscape(sessionID) + "/stream"
	return parsed.String(), nil
}

func (c *realtimeConn) waitReady(ctx context.Context) error {
	for {
		event, err := c.Read(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case realtime.EventSessionReady:
			return nil
		case realtime.EventError:
			return fmt.Errorf("realtime error before ready: %s", string(event.Payload))
		}
	}
}

func (c *realtimeConn) Read(ctx context.Context) (realtime.Event, error) {
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			return realtime.Event{}, err
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		var event realtime.Event
		if err := json.Unmarshal(data, &event); err != nil {
			return realtime.Event{}, err
		}
		return event, nil
	}
}

func (c *realtimeConn) SendAudioPCM(ctx context.Context, pcm []byte, sampleRateHz int) error {
	if len(pcm) == 0 {
		return nil
	}
	if sampleRateHz <= 0 {
		sampleRateHz = 16000
	}
	return c.writeEvent(ctx, realtime.EventInputAudioAppend, realtime.InputAudioPayload{
		AudioBase64: base64.StdEncoding.EncodeToString(pcm),
		MIMEType:    fmt.Sprintf("audio/pcm;rate=%d", sampleRateHz),
		DurationMs:  len(pcm) / 2 * 1000 / sampleRateHz,
	})
}

func (c *realtimeConn) SendAudioEnd(ctx context.Context) error {
	return c.writeEvent(ctx, realtime.EventInputAudioEnd, map[string]any{})
}

func (c *realtimeConn) SendText(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return c.writeEvent(ctx, realtime.EventInputTextAppend, realtime.InputTextPayload{Text: text, EndOfTurn: true})
}

func (c *realtimeConn) Close(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return nil
	}
	_ = c.writeEvent(ctx, realtime.EventSessionClose, map[string]any{})
	return c.conn.Close(websocket.StatusNormalClosure, "done")
}

func (c *realtimeConn) writeEvent(ctx context.Context, eventType realtime.EventType, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := realtime.Event{
		V:              realtime.ProtocolVersion,
		Type:           eventType,
		VoiceSessionID: c.Session.ID,
		Payload:        body,
		At:             time.Now().UTC(),
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, data)
}
