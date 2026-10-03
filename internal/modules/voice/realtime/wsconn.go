package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/coder/websocket"
)

const (
	dialTimeout       = 20 * time.Second
	maxWSMessageBytes = 8 << 20
)

// wsConnection is a provider session: one websocket driven by the provider's
// codec.
type wsConnection struct {
	name      string
	conn      *websocket.Conn
	codec     Codec
	writeMu   sync.Mutex
	outputs   chan ProviderOutput
	closeOnce sync.Once
}

// connect dials spec's websocket, runs the codec's setup handshake and starts
// reading provider output.
func connect(ctx context.Context, spec ProviderSpec, cfg ProviderConfig, req ProviderConnectRequest) (ProviderConnection, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%s: %w", spec.Name, errAPIKeyRequired)
	}
	if spec.Models != nil && !spec.hasModel(req.ModelID, spec.Models) {
		return nil, fmt.Errorf("%s: model %q is not available", spec.Name, req.ModelID)
	}
	endpoint, header, err := spec.Dial(cfg, req.ModelID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.Name, err)
	}
	setupCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(setupCtx, endpoint, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, fmt.Errorf("%s: websocket dial: %w", spec.Name, err)
	}
	conn.SetReadLimit(maxWSMessageBytes)
	c := &wsConnection{name: spec.Name, conn: conn, codec: spec.NewCodec(req), outputs: make(chan ProviderOutput, 64)}
	if err := c.handshake(setupCtx); err != nil {
		_ = c.Close(err)
		return nil, err
	}
	safego.Go("realtime.providerReadLoop", func() { c.readLoop(ctx) })
	return c, nil
}

func (c *wsConnection) handshake(ctx context.Context) error {
	messages, err := c.codec.Setup()
	if err != nil {
		return fmt.Errorf("%s: %w", c.name, err)
	}
	for _, message := range messages {
		if err := c.write(ctx, message); err != nil {
			return fmt.Errorf("%s: send setup: %w", c.name, err)
		}
	}
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("%s: wait for setup: %w", c.name, err)
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		done, err := c.codec.SetupDone(data)
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		if done {
			return nil
		}
	}
}

func (c *wsConnection) Send(ctx context.Context, input ProviderInput) error {
	messages, err := c.codec.Encode(input)
	if err != nil {
		return fmt.Errorf("%s: %w", c.name, err)
	}
	for _, message := range messages {
		if err := c.write(ctx, message); err != nil {
			return err
		}
	}
	return nil
}

func (c *wsConnection) Receive(ctx context.Context) (ProviderOutput, error) {
	select {
	case output, ok := <-c.outputs:
		if !ok {
			return ProviderOutput{}, io.EOF
		}
		return output, nil
	case <-ctx.Done():
		return ProviderOutput{}, ctx.Err()
	}
}

func (c *wsConnection) Close(reason error) error {
	var err error
	c.closeOnce.Do(func() {
		status, message := websocket.StatusNormalClosure, ""
		if reason != nil {
			status, message = websocket.StatusInternalError, truncate(strings.TrimSpace(reason.Error()), 120)
		}
		err = c.conn.Close(status, message)
	})
	return err
}

func (c *wsConnection) readLoop(ctx context.Context) {
	defer close(c.outputs)
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				c.emit(ctx, ProviderOutput{Type: ProviderOutputError, Error: err.Error()})
			}
			return
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		for _, output := range c.codec.Decode(data) {
			c.emit(ctx, output)
		}
	}
}

func (c *wsConnection) emit(ctx context.Context, output ProviderOutput) {
	select {
	case c.outputs <- output:
	case <-ctx.Done():
	}
}

func (c *wsConnection) write(ctx context.Context, message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, body)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
