package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/coder/websocket"
)

type connection struct {
	conn        *websocket.Conn
	writeMu     sync.Mutex
	decoder     *messageDecoder
	resampler   *pcm16Resampler
	outputs     chan realtime.ProviderOutput
	closeOnce   sync.Once
	outputsOnce sync.Once
}

func (c *connection) Send(ctx context.Context, input realtime.ProviderInput) error {
	switch input.Type {
	case realtime.ProviderInputAudioAppend:
		return c.sendAudio(ctx, input.AudioBase64)
	case realtime.ProviderInputAudioEnd:
		return c.flushAudio(ctx)
	case realtime.ProviderInputTextAppend:
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil
		}
		if err := c.writeJSON(ctx, textMessage(text)); err != nil {
			return err
		}
		if input.EndOfTurn {
			return c.writeJSON(ctx, responseCreateMessage())
		}
		return nil
	case realtime.ProviderInputCancel:
		return c.writeJSON(ctx, map[string]any{"type": "response.cancel"})
	case realtime.ProviderInputToolResult:
		for _, response := range input.ToolResponses {
			body, err := json.Marshal(response.Response)
			if err != nil {
				body = []byte(`{"error":"could not encode tool result"}`)
			}
			if err := c.writeJSON(ctx, toolResultMessage(response.ID, body)); err != nil {
				return err
			}
		}
		if len(input.ToolResponses) > 0 {
			return c.writeJSON(ctx, responseCreateMessage())
		}
		return nil
	default:
		return nil
	}
}

func (c *connection) Receive(ctx context.Context) (realtime.ProviderOutput, error) {
	select {
	case output, ok := <-c.outputs:
		if !ok {
			return realtime.ProviderOutput{}, io.EOF
		}
		return output, nil
	case <-ctx.Done():
		return realtime.ProviderOutput{}, ctx.Err()
	}
}

func (c *connection) Close(reason error) error {
	var err error
	c.closeOnce.Do(func() {
		status := websocket.StatusNormalClosure
		message := ""
		if reason != nil {
			status = websocket.StatusInternalError
			message = truncateReason(reason.Error())
		}
		err = c.conn.Close(status, message)
	})
	return err
}

func (c *connection) sendAudio(ctx context.Context, encoded string) error {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	audio, err := decodeBase64Audio(encoded)
	if err != nil {
		return fmt.Errorf("openai realtime: decode input audio: %w", err)
	}
	converted, err := c.resampler.Convert(audio)
	if err != nil {
		return fmt.Errorf("openai realtime: resample input audio: %w", err)
	}
	if len(converted) == 0 {
		return nil
	}
	return c.writeJSON(ctx, audioAppendMessage(converted))
}

func (c *connection) flushAudio(ctx context.Context) error {
	audio := c.resampler.Flush()
	if len(audio) == 0 {
		return nil
	}
	return c.writeJSON(ctx, audioAppendMessage(audio))
}

func (c *connection) waitSessionUpdated(ctx context.Context) error {
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("openai realtime: wait session update: %w", err)
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		var msg serverMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("openai realtime: decode setup response: %w", err)
		}
		switch strings.TrimSpace(msg.Type) {
		case "session.updated":
			return nil
		case "error":
			return fmt.Errorf("openai realtime: %s", serverErrorMessage(msg.Error, "session update failed"))
		default:
			continue
		}
	}
}

func (c *connection) readLoop(ctx context.Context) {
	defer c.closeOutputs()
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}
			c.emit(ctx, realtime.ProviderOutput{Type: realtime.ProviderOutputError, Error: err.Error()})
			return
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		for _, output := range c.decoder.Decode(data) {
			c.emit(ctx, output)
		}
	}
}

func (c *connection) closeOutputs() {
	c.outputsOnce.Do(func() { close(c.outputs) })
}

func (c *connection) emit(ctx context.Context, output realtime.ProviderOutput) {
	select {
	case c.outputs <- output:
	case <-ctx.Done():
	}
}

func (c *connection) writeJSON(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, body)
}
