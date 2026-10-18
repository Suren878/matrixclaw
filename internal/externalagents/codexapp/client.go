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
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

// Conn is the app-server's stdio; writes take a deadline so a wedged server
// fails the write instead of stalling its caller.
type Conn interface {
	io.ReadWriteCloser
	SetWriteDeadline(t time.Time) error
}

type connErrorProvider interface {
	ProcessError() error
}

type stdoutClosedErrorProvider interface {
	StdoutClosedError() error
}

type Client struct {
	conn Conn

	writeMu sync.Mutex
	nextID  atomic.Uint64

	pendingMu sync.Mutex
	pending   map[string]chan rpcReply

	done  chan struct{}
	errMu sync.Mutex
	err   error

	routeMu     sync.Mutex
	turnSubs    map[turnKey]*TurnSubscription
	turnBacklog map[turnKey]*turnBacklog
	routeClosed bool
	now         func() time.Time
}

type turnKey struct {
	threadID string
	turnID   string
}

// turnBacklog holds a turn's notifications that arrived before its
// subscription (between turn/start and SubscribeTurn).
type turnBacklog struct {
	events []Notification
	last   time.Time
}

const (
	turnBacklogLimit = 256
	// turnBacklogTTL is how long a turn nobody subscribed to keeps its backlog.
	turnBacklogTTL = time.Minute
	writeTimeout   = 30 * time.Second
)

// TurnSubscription is one turn's queue, filled by the read loop and drained
// by Next.
type TurnSubscription struct {
	client  *Client
	key     turnKey
	mu      sync.Mutex
	pending []Notification
	closed  bool
	wake    chan struct{}
}

type rpcRequest struct {
	ID     string `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
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
		conn:        conn,
		pending:     map[string]chan rpcReply{},
		done:        make(chan struct{}),
		turnSubs:    map[turnKey]*TurnSubscription{},
		turnBacklog: map[turnKey]*turnBacklog{},
		now:         time.Now,
	}
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

func (c *Client) InterruptTurn(ctx context.Context, params TurnInterruptParams) (TurnInterruptResponse, error) {
	var out TurnInterruptResponse
	if err := c.Call(ctx, "turn/interrupt", params, &out); err != nil {
		return TurnInterruptResponse{}, err
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

// SubscribeTurn starts the turn's queue with what arrived for it so far.
// Close the subscription when done with the turn.
func (c *Client) SubscribeTurn(threadID, turnID string) *TurnSubscription {
	key := turnKey{threadID: threadID, turnID: turnID}
	sub := &TurnSubscription{client: c, key: key, wake: make(chan struct{}, 1)}
	c.routeMu.Lock()
	defer c.routeMu.Unlock()
	if c.routeClosed {
		sub.closed = true
		return sub
	}
	if backlog := c.turnBacklog[key]; backlog != nil {
		sub.pending = backlog.events
		delete(c.turnBacklog, key)
	}
	if previous := c.turnSubs[key]; previous != nil {
		previous.end()
	}
	c.turnSubs[key] = sub
	return sub
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

// write sends one message under writeTimeout (or ctx's earlier deadline). A
// failed write may have sent part of a line, so it closes the connection.
func (c *Client) write(ctx context.Context, req any) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode codex app-server request: %w", err)
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := c.now().Add(writeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("write codex app-server request: %w", err)
	}
	if _, err := c.conn.Write(data); err != nil {
		err = fmt.Errorf("write codex app-server request: %w", err)
		c.setErr(err)
		_ = c.conn.Close()
		return err
	}
	return nil
}

func (c *Client) readLoop() {
	defer close(c.done)
	defer c.closeTurnSubscriptions()
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
		c.setErr(err)
	}
}

func (c *Client) readLoopMessages() error {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var msg rpcIncoming
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return fmt.Errorf("decode codex app-server message: %w", err)
		}
		switch {
		case len(msg.ID) > 0 && msg.Method != "":
			c.answerServerRequest(msg)
		case len(msg.ID) > 0:
			if err := c.handleReply(msg); err != nil {
				return err
			}
		case msg.Method != "":
			c.route(decodeNotification(msg.Method, msg.Params))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, os.ErrClosed) {
		return fmt.Errorf("read codex app-server message: %w", err)
	}
	if provider, ok := c.conn.(connErrorProvider); ok {
		if err := provider.ProcessError(); err != nil {
			return err
		}
		if closedProvider, ok := c.conn.(stdoutClosedErrorProvider); ok {
			return closedProvider.StdoutClosedError()
		}
		return fmt.Errorf("codex app-server stdout closed")
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

// answerServerRequest declines every approval codex asks for: matrixclaw has no
// way to ask the user mid-turn, so codex reports the item as declined and the
// turn goes on. Anything else gets method-not-found so codex never waits.
// The answer is written off the read loop, which must keep reading meanwhile.
func (c *Client) answerServerRequest(msg rpcIncoming) {
	resp := rpcResponse{ID: msg.ID}
	switch msg.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		resp.Result = map[string]string{"decision": "decline"}
	case "execCommandApproval", "applyPatchApproval":
		resp.Result = map[string]string{"decision": "denied"}
	case "mcpServer/elicitation/request":
		resp.Result = map[string]string{"action": "decline"}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "matrixclaw does not support " + msg.Method}
	}
	safego.Go("codexapp.answerServerRequest", func() {
		_ = c.write(context.Background(), resp)
	})
}

// route puts a notification in its turn's queue, or in the turn's backlog
// while nobody subscribed. A notification the client could not decode goes
// to every turn it may belong to.
func (c *Client) route(event Notification) {
	c.routeMu.Lock()
	defer c.routeMu.Unlock()
	if c.routeClosed {
		return
	}
	if failure, ok := event.Params.(DecodeFailure); ok && failure.TurnID == "" {
		for key, sub := range c.turnSubs {
			if failure.ThreadID == "" || failure.ThreadID == key.threadID {
				sub.enqueue(event)
			}
		}
		return
	}
	key, ok := notificationTurnKey(event)
	if !ok {
		return
	}
	if sub := c.turnSubs[key]; sub != nil {
		sub.enqueue(event)
		return
	}
	now := c.now()
	for other, backlog := range c.turnBacklog {
		if now.Sub(backlog.last) > turnBacklogTTL {
			delete(c.turnBacklog, other)
		}
	}
	backlog := c.turnBacklog[key]
	if backlog == nil {
		backlog = &turnBacklog{}
		c.turnBacklog[key] = backlog
	}
	backlog.events = appendTurnNotification(backlog.events, event, turnBacklogLimit)
	backlog.last = now
}

// Close ends the subscription and forgets its turn; later notifications of
// the turn are backlogged until turnBacklogTTL drops them.
func (s *TurnSubscription) Close() {
	c := s.client
	c.routeMu.Lock()
	if c.turnSubs[s.key] == s {
		delete(c.turnSubs, s.key)
	}
	c.routeMu.Unlock()
	s.end()
}

// Next returns the turn's next notification; false once the subscription
// ended and is drained, or ctx is done.
func (s *TurnSubscription) Next(ctx context.Context) (Notification, bool) {
	for {
		s.mu.Lock()
		if len(s.pending) > 0 {
			event := s.pending[0]
			s.pending[0] = Notification{}
			s.pending = s.pending[1:]
			s.mu.Unlock()
			return event, true
		}
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return Notification{}, false
		}
		select {
		case <-ctx.Done():
			return Notification{}, false
		case <-s.wake:
		}
	}
}

func (s *TurnSubscription) enqueue(event Notification) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.pending = appendTurnNotification(s.pending, event, turnBacklogLimit)
	s.mu.Unlock()
	s.signal()
}

// end stops the queue; what is queued can still be read.
func (s *TurnSubscription) end() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.signal()
}

func (s *TurnSubscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// appendTurnNotification keeps control events lossless while bounding the
// common high-volume case. Consecutive text deltas for the same item are
// coalesced, and stale patch snapshots are replaced after the soft limit. The
// limit is deliberately soft: an unusual sequence of non-coalescible control
// events is retained instead of losing a terminal event.
func appendTurnNotification(queue []Notification, event Notification, softLimit int) []Notification {
	if len(queue) > 0 {
		if merged, ok := mergeTurnNotifications(queue[len(queue)-1], event); ok {
			queue[len(queue)-1] = merged
			return queue
		}
	}
	if len(queue) >= softLimit {
		if replaceTurnSnapshot(queue, event) {
			return queue
		}
	}
	return append(queue, event)
}

func mergeTurnNotifications(previous Notification, next Notification) (Notification, bool) {
	if previous.Method != next.Method {
		return Notification{}, false
	}
	switch left := previous.Params.(type) {
	case AgentMessageDelta:
		right, ok := next.Params.(AgentMessageDelta)
		if !ok || left.ThreadID != right.ThreadID || left.TurnID != right.TurnID || left.ItemID != right.ItemID {
			return Notification{}, false
		}
		left.Delta += right.Delta
		previous.Params = left
		return previous, true
	case ReasoningTextDelta:
		right, ok := next.Params.(ReasoningTextDelta)
		if !ok || left.ThreadID != right.ThreadID || left.TurnID != right.TurnID || left.ItemID != right.ItemID {
			return Notification{}, false
		}
		left.Delta += right.Delta
		previous.Params = left
		return previous, true
	case ToolOutputDelta:
		right, ok := next.Params.(ToolOutputDelta)
		if !ok || left.ThreadID != right.ThreadID || left.TurnID != right.TurnID || left.ItemID != right.ItemID {
			return Notification{}, false
		}
		left.Delta += right.Delta
		previous.Params = left
		return previous, true
	case FileChangePatchUpdated:
		right, ok := next.Params.(FileChangePatchUpdated)
		if !ok || left.ThreadID != right.ThreadID || left.TurnID != right.TurnID || left.ItemID != right.ItemID {
			return Notification{}, false
		}
		return next, true
	}
	return Notification{}, false
}

func replaceTurnSnapshot(queue []Notification, event Notification) bool {
	next, ok := event.Params.(FileChangePatchUpdated)
	if !ok {
		return false
	}
	for i := len(queue) - 1; i >= 0; i-- {
		current, ok := queue[i].Params.(FileChangePatchUpdated)
		if !ok {
			continue
		}
		if current.ThreadID == next.ThreadID && current.TurnID == next.TurnID && current.ItemID == next.ItemID {
			queue[i] = event
			return true
		}
	}
	return false
}

func notificationTurnKey(event Notification) (turnKey, bool) {
	var key turnKey
	switch params := event.Params.(type) {
	case ItemNotification:
		key = turnKey{params.ThreadID, params.TurnID}
	case AgentMessageDelta:
		key = turnKey{params.ThreadID, params.TurnID}
	case ReasoningTextDelta:
		key = turnKey{params.ThreadID, params.TurnID}
	case ToolOutputDelta:
		key = turnKey{params.ThreadID, params.TurnID}
	case FileChangePatchUpdated:
		key = turnKey{params.ThreadID, params.TurnID}
	case TurnCompleted:
		key = turnKey{params.ThreadID, params.Turn.ID}
	case TurnStarted:
		key = turnKey{params.ThreadID, params.Turn.ID}
	case ErrorNotification:
		key = turnKey{params.ThreadID, params.TurnID}
	case ContextCompactedNotification:
		key = turnKey{params.ThreadID, params.TurnID}
	case DecodeFailure:
		key = turnKey{params.ThreadID, params.TurnID}
	default:
		return turnKey{}, false
	}
	return key, key.threadID != "" && key.turnID != ""
}

func (c *Client) closeTurnSubscriptions() {
	c.routeMu.Lock()
	c.routeClosed = true
	subs := c.turnSubs
	c.turnSubs = map[turnKey]*TurnSubscription{}
	c.turnBacklog = map[turnKey]*turnBacklog{}
	c.routeMu.Unlock()
	for _, sub := range subs {
		sub.end()
	}
}

// decodeNotification decodes the params of the notifications the runtime
// reads. Params it cannot decode become a DecodeFailure for their turn, so
// the turn fails instead of waiting for an end it would never see.
func decodeNotification(method string, raw json.RawMessage) Notification {
	switch method {
	case "item/started", "item/completed":
		return decodeParams[ItemNotification](method, raw)
	case "item/agentMessage/delta":
		return decodeParams[AgentMessageDelta](method, raw)
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		return decodeParams[ReasoningTextDelta](method, raw)
	case "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
		return decodeParams[ToolOutputDelta](method, raw)
	case "item/fileChange/patchUpdated":
		return decodeParams[FileChangePatchUpdated](method, raw)
	case "turn/completed":
		return decodeParams[TurnCompleted](method, raw)
	case "turn/started":
		return decodeParams[TurnStarted](method, raw)
	case "error":
		return decodeParams[ErrorNotification](method, raw)
	case "thread/compacted":
		return decodeParams[ContextCompactedNotification](method, raw)
	default:
		return Notification{Method: method, Params: raw}
	}
}

func decodeParams[T any](method string, raw json.RawMessage) Notification {
	var params T
	if err := json.Unmarshal(raw, &params); err != nil {
		var ids struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(raw, &ids)
		turnID := ids.TurnID
		if turnID == "" {
			turnID = ids.Turn.ID
		}
		return Notification{Method: method, Params: DecodeFailure{ThreadID: ids.ThreadID, TurnID: turnID, Err: err}}
	}
	return Notification{Method: method, Params: params}
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
