package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/safego"
)

type Conn interface {
	io.Reader
	io.Writer
	io.Closer
}

type connErrorProvider interface {
	ProcessError() error
}

type Client struct {
	conn Conn
	enc  *json.Encoder

	writeMu sync.Mutex
	nextID  atomic.Uint64

	pendingMu sync.Mutex
	pending   map[string]chan rpcReply

	events chan Notification
	done   chan struct{}
	errMu  sync.Mutex
	err    error

	routeMu     sync.Mutex
	turnSubs    map[turnKey]map[chan Notification]struct{}
	turnBacklog map[turnKey][]Notification
	routeDone   chan struct{}
	routeClosed bool
}

type turnKey struct {
	threadID string
	turnID   string
}

const (
	turnBacklogLimit       = 256
	turnSubscriptionBuffer = 64
)

type rpcRequest struct {
	ID     string `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcIncoming struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcReply struct {
	result json.RawMessage
	err    error
}

func NewClient(conn Conn) *Client {
	c := &Client{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		pending: map[string]chan rpcReply{},
		events:  make(chan Notification, 128),
		done:    make(chan struct{}),

		turnSubs:    map[turnKey]map[chan Notification]struct{}{},
		turnBacklog: map[turnKey][]Notification{},
		routeDone:   make(chan struct{}),
	}
	safego.Go("codexapp.eventRouter", c.routeEvents)
	safego.Go("codexapp.readLoop", c.readLoop)
	return c
}

func (c *Client) Initialize(ctx context.Context, params InitializeParams) (InitializeResponse, error) {
	var out InitializeResponse
	if err := c.Call(ctx, "initialize", params, &out); err != nil {
		return InitializeResponse{}, err
	}
	if err := c.Notify(ctx, "initialized", nil); err != nil {
		return InitializeResponse{}, err
	}
	return out, nil
}

func (c *Client) StartThread(ctx context.Context, params ThreadStartParams) (ThreadStartResponse, error) {
	var out ThreadStartResponse
	if err := c.Call(ctx, "thread/start", params, &out); err != nil {
		return ThreadStartResponse{}, err
	}
	return out, nil
}

func (c *Client) ResumeThread(ctx context.Context, params ThreadResumeParams) (ThreadResumeResponse, error) {
	var out ThreadResumeResponse
	if err := c.Call(ctx, "thread/resume", params, &out); err != nil {
		return ThreadResumeResponse{}, err
	}
	return out, nil
}

func (c *Client) StartTurn(ctx context.Context, params TurnStartParams) (TurnStartResponse, error) {
	var out TurnStartResponse
	if err := c.Call(ctx, "turn/start", params, &out); err != nil {
		return TurnStartResponse{}, err
	}
	return out, nil
}

func (c *Client) SteerTurn(ctx context.Context, params TurnSteerParams) (TurnSteerResponse, error) {
	var out TurnSteerResponse
	if err := c.Call(ctx, "turn/steer", params, &out); err != nil {
		return TurnSteerResponse{}, err
	}
	return out, nil
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	replyCh := make(chan rpcReply, 1)
	c.pendingMu.Lock()
	c.pending[id] = replyCh
	c.pendingMu.Unlock()
	defer c.forget(id)

	if err := c.write(ctx, rpcRequest{ID: id, Method: method, Params: params}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		if err := c.Err(); err != nil {
			return err
		}
		return io.ErrClosedPipe
	case reply := <-replyCh:
		if reply.err != nil {
			return reply.err
		}
		if out == nil || len(reply.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(reply.result, out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) Notify(ctx context.Context, method string, params any) error {
	return c.write(ctx, rpcRequest{Method: method, Params: params})
}

func (c *Client) Events() <-chan Notification {
	return c.events
}

func (c *Client) SubscribeTurn(ctx context.Context, threadID, turnID string) (<-chan Notification, func()) {
	key := turnKey{threadID: threadID, turnID: turnID}
	ch := make(chan Notification, turnSubscriptionBuffer)

	c.routeMu.Lock()
	if c.routeClosed {
		c.routeMu.Unlock()
		close(ch)
		return ch, func() {}
	}
	if c.turnSubs == nil {
		c.turnSubs = map[turnKey]map[chan Notification]struct{}{}
	}
	if c.turnBacklog == nil {
		c.turnBacklog = map[turnKey][]Notification{}
	}
	subs := c.turnSubs[key]
	if subs == nil {
		subs = map[chan Notification]struct{}{}
		c.turnSubs[key] = subs
	}
	subs[ch] = struct{}{}
	backlog := append([]Notification(nil), c.turnBacklog[key]...)
	delete(c.turnBacklog, key)
	c.routeMu.Unlock()

	if err := deliverBacklog(ch, backlog); err != nil {
		c.fail(err)
	}

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			c.unsubscribeTurn(key, ch)
		})
	}
	if ctx != nil {
		safego.Go("codexapp.turnSubscription", func() {
			select {
			case <-ctx.Done():
				unsubscribe()
			case <-c.routeDone:
			}
		})
	}
	return ch, unsubscribe
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

func (c *Client) write(ctx context.Context, req rpcRequest) error {
	done := make(chan error, 1)
	safego.Go("codexapp.write", func() {
		if !safego.Run("codexapp.write", func() {
			c.writeMu.Lock()
			defer c.writeMu.Unlock()
			done <- c.enc.Encode(req)
		}) {
			done <- fmt.Errorf("codex app-server write panicked")
		}
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("write codex app-server request: %w", err)
		}
		return nil
	}
}

func (c *Client) readLoop() {
	defer close(c.events)
	defer close(c.done)
	defer func() {
		c.failPending(c.Err())
	}()

	var err error
	if !safego.Run("codexapp.readLoop", func() {
		err = c.readLoopMessages()
	}) {
		err = fmt.Errorf("codex app-server read loop panicked")
	}
	if err != nil {
		c.fail(err)
	}
}

func (c *Client) readLoopMessages() error {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var msg rpcIncoming
		if err := json.Unmarshal(line, &msg); err != nil {
			return fmt.Errorf("decode codex app-server message: %w", err)
		}
		if len(msg.ID) > 0 {
			if err := c.handleReply(msg); err != nil {
				return err
			}
			continue
		}
		if msg.Method != "" {
			if err := c.handleNotification(msg, line); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, os.ErrClosed) {
		return fmt.Errorf("read codex app-server message: %w", err)
	}
	if provider, ok := c.conn.(connErrorProvider); ok {
		if err := provider.ProcessError(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) handleReply(msg rpcIncoming) error {
	id, err := decodeID(msg.ID)
	if err != nil {
		return err
	}
	c.pendingMu.Lock()
	replyCh := c.pending[id]
	c.pendingMu.Unlock()
	if replyCh == nil {
		return nil
	}
	if msg.Error != nil {
		replyCh <- rpcReply{err: fmt.Errorf("codex app-server %s: %s", id, msg.Error.Message)}
		return nil
	}
	replyCh <- rpcReply{result: msg.Result}
	return nil
}

func (c *Client) handleNotification(msg rpcIncoming, raw []byte) error {
	notification := Notification{
		Method: msg.Method,
		Params: decodeNotificationParams(msg.Method, msg.Params),
		Raw:    raw,
	}
	select {
	case c.events <- notification:
		return nil
	default:
		return fmt.Errorf("codex app-server event buffer full")
	}
}

func (c *Client) routeEvents() {
	defer close(c.routeDone)
	defer c.closeTurnSubscriptions()
	if !safego.Run("codexapp.eventRouter", c.routeEventsLoop) {
		c.fail(fmt.Errorf("codex app-server event router panicked"))
	}
}

func (c *Client) routeEventsLoop() {
	for event := range c.events {
		if err := c.routeNotification(event); err != nil {
			c.fail(err)
			return
		}
	}
}

func (c *Client) routeNotification(event Notification) error {
	key, ok := notificationTurnKey(event)
	if !ok {
		return nil
	}
	c.routeMu.Lock()
	defer c.routeMu.Unlock()
	if c.routeClosed {
		return nil
	}
	subs := c.turnSubs[key]
	if len(subs) == 0 {
		backlog := c.turnBacklog[key]
		if len(backlog) >= turnBacklogLimit {
			return fmt.Errorf("codex app-server event backlog full for thread %q turn %q", key.threadID, key.turnID)
		}
		c.turnBacklog[key] = append(backlog, event)
		return nil
	}
	for sub := range subs {
		select {
		case sub <- event:
		default:
			return fmt.Errorf("codex app-server turn subscription buffer full for thread %q turn %q", key.threadID, key.turnID)
		}
	}
	return nil
}

func deliverBacklog(ch chan Notification, backlog []Notification) error {
	for _, event := range backlog {
		select {
		case ch <- event:
		default:
			return fmt.Errorf("codex app-server turn subscription buffer full")
		}
	}
	return nil
}

func notificationTurnKey(event Notification) (turnKey, bool) {
	switch params := event.Params.(type) {
	case ItemNotification:
		return turnKey{threadID: params.ThreadID, turnID: params.TurnID}, params.ThreadID != "" && params.TurnID != ""
	case AgentMessageDelta:
		return turnKey{threadID: params.ThreadID, turnID: params.TurnID}, params.ThreadID != "" && params.TurnID != ""
	case ReasoningTextDelta:
		return turnKey{threadID: params.ThreadID, turnID: params.TurnID}, params.ThreadID != "" && params.TurnID != ""
	case ToolOutputDelta:
		return turnKey{threadID: params.ThreadID, turnID: params.TurnID}, params.ThreadID != "" && params.TurnID != ""
	case FileChangePatchUpdated:
		return turnKey{threadID: params.ThreadID, turnID: params.TurnID}, params.ThreadID != "" && params.TurnID != ""
	case TurnCompleted:
		return turnKey{threadID: params.ThreadID, turnID: params.Turn.ID}, params.ThreadID != "" && params.Turn.ID != ""
	default:
		return turnKey{}, false
	}
}

func (c *Client) unsubscribeTurn(key turnKey, ch chan Notification) {
	c.routeMu.Lock()
	if subs := c.turnSubs[key]; subs != nil {
		if _, ok := subs[ch]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(c.turnSubs, key)
			}
			c.routeMu.Unlock()
			close(ch)
			return
		}
	}
	c.routeMu.Unlock()
}

func (c *Client) closeTurnSubscriptions() {
	c.routeMu.Lock()
	if c.routeClosed {
		c.routeMu.Unlock()
		return
	}
	c.routeClosed = true
	var subs []chan Notification
	for _, byTurn := range c.turnSubs {
		for sub := range byTurn {
			subs = append(subs, sub)
		}
	}
	c.turnSubs = map[turnKey]map[chan Notification]struct{}{}
	c.turnBacklog = map[turnKey][]Notification{}
	c.routeMu.Unlock()
	for _, sub := range subs {
		close(sub)
	}
}

func decodeNotificationParams(method string, raw json.RawMessage) any {
	switch method {
	case "item/started", "item/completed":
		var params ItemNotification
		if json.Unmarshal(raw, &params) == nil {
			return params
		}
	case "item/agentMessage/delta":
		var params AgentMessageDelta
		if json.Unmarshal(raw, &params) == nil {
			return params
		}
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		var params ReasoningTextDelta
		if json.Unmarshal(raw, &params) == nil {
			return params
		}
	case "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
		var params ToolOutputDelta
		if json.Unmarshal(raw, &params) == nil {
			return params
		}
	case "item/fileChange/patchUpdated":
		var params FileChangePatchUpdated
		if json.Unmarshal(raw, &params) == nil {
			return params
		}
	case "turn/completed":
		var params TurnCompleted
		if json.Unmarshal(raw, &params) == nil {
			return params
		}
	}
	return raw
}

func decodeID(raw json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return strconv.FormatInt(n, 10), nil
	}
	return "", fmt.Errorf("unsupported codex app-server response id: %s", string(raw))
}

func (c *Client) forget(id string) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) failPending(err error) {
	if err == nil {
		err = io.ErrClosedPipe
	}
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- rpcReply{err: err}
		delete(c.pending, id)
	}
}

func (c *Client) setErr(err error) {
	if err == nil {
		return
	}
	c.errMu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.errMu.Unlock()
}

func (c *Client) fail(err error) {
	if err == nil {
		return
	}
	c.setErr(err)
	if c.conn != nil {
		_ = c.conn.Close()
	}
}
