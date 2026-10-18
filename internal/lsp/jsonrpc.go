package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/safego"
)

// maxMessage bounds one message a server sends.
const maxMessage = 64 << 20

// message is one JSON-RPC 2.0 request, notification or response.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

const codeMethodNotFound = -32601

// conn is a JSON-RPC 2.0 connection with Content-Length framing. Notifications
// are handled in order on the reading goroutine; requests from the server are
// answered on their own goroutines.
type conn struct {
	write     func(func(io.Writer) error) error
	onNotify  func(method string, params json.RawMessage)
	onRequest func(method string, params json.RawMessage) (any, *rpcError)

	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan message
	done    chan struct{}
	err     error
}

// newConn reads r until it ends; write sends one framed message at a time.
func newConn(r io.Reader, write func(func(io.Writer) error) error, onNotify func(string, json.RawMessage), onRequest func(string, json.RawMessage) (any, *rpcError)) *conn {
	c := &conn{write: write, onNotify: onNotify, onRequest: onRequest, pending: map[int64]chan message{}, done: make(chan struct{})}
	safego.Go("lsp.read", func() { c.close(c.read(bufio.NewReader(r))) })
	return c
}

// call sends a request and decodes its result into result (nil discards it).
// A canceled ctx cancels the request on the server.
func (c *conn) call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	reply := make(chan message, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(message{ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method}, params); err != nil {
		return err
	}
	select {
	case msg := <-reply:
		if msg.Error != nil {
			return fmt.Errorf("%s: %w", method, msg.Error)
		}
		if result == nil || len(msg.Result) == 0 {
			return nil
		}
		return json.Unmarshal(msg.Result, result)
	case <-ctx.Done():
		_ = c.notify("$/cancelRequest", map[string]int64{"id": id})
		return fmt.Errorf("%s: %w", method, context.Cause(ctx))
	case <-c.done:
		return c.closedErr()
	}
}

func (c *conn) notify(method string, params any) error {
	return c.send(message{Method: method}, params)
}

func (c *conn) send(msg message, params any) error {
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		msg.Params = raw
	}
	return c.sendRaw(msg)
}

// notification is a notification message whose params cannot fail to encode.
func notification(method string, params any) message {
	raw, _ := json.Marshal(params)
	return message{Method: method, Params: raw}
}

func (c *conn) sendRaw(msg message) error {
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.write(func(w io.Writer) error {
		if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
			return err
		}
		_, err := w.Write(body)
		return err
	})
}

func (c *conn) read(r *bufio.Reader) error {
	headers := textproto.NewReader(r)
	for {
		header, err := headers.ReadMIMEHeader()
		if err != nil {
			return err
		}
		length, err := strconv.Atoi(strings.TrimSpace(header.Get("Content-Length")))
		if err != nil || length < 0 || length > maxMessage {
			return fmt.Errorf("bad Content-Length %q", header.Get("Content-Length"))
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(r, body); err != nil {
			return err
		}
		var msg message
		if err := json.Unmarshal(body, &msg); err != nil {
			return fmt.Errorf("bad message: %w", err)
		}
		c.dispatch(msg)
	}
}

func (c *conn) dispatch(msg message) {
	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		safego.Go("lsp.request", func() {
			result, rpcErr := c.onRequest(msg.Method, msg.Params)
			reply := message{ID: msg.ID, Error: rpcErr}
			if rpcErr == nil {
				raw, err := json.Marshal(result)
				if err != nil {
					raw = json.RawMessage("null")
				}
				reply.Result = raw
			}
			_ = c.sendRaw(reply)
		})
	case msg.Method != "":
		c.onNotify(msg.Method, msg.Params)
	default:
		id, err := strconv.ParseInt(string(msg.ID), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		reply := c.pending[id]
		c.mu.Unlock()
		if reply != nil {
			if msg.Error == nil && msg.Result == nil {
				msg.Result = json.RawMessage("null")
			}
			reply <- msg
		}
	}
}

func (c *conn) close(err error) {
	if errors.Is(err, io.EOF) || err == nil {
		err = errors.New("the language server closed the connection")
	}
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	close(c.done)
}

func (c *conn) closedErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
