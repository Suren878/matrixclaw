package codexapp

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

func TestClientReadLoopPanicClosesEventsAndSetsError(t *testing.T) {
	client := NewClient(panicReadConn{})

	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatalf("events channel delivered a notification after read panic")
		}
	case <-time.After(time.Second):
		t.Fatalf("events channel did not close after read panic")
	}

	select {
	case <-client.done:
	case <-time.After(time.Second):
		t.Fatalf("done channel did not close after read panic")
	}

	err := client.Err()
	if err == nil {
		t.Fatalf("Err() is nil after read panic")
	}
	if !strings.Contains(err.Error(), "codex app-server read loop panicked") {
		t.Fatalf("Err() = %q, want read loop panic", err)
	}
}

func TestClientSubscribeTurnRoutesInterleavedEvents(t *testing.T) {
	client := newRoutingClientForTest(t)

	turn1, unsubscribe1 := client.SubscribeTurn(context.Background(), "thread-1", "turn-1")
	defer unsubscribe1()
	turn2, unsubscribe2 := client.SubscribeTurn(context.Background(), "thread-1", "turn-2")
	defer unsubscribe2()
	waitForSubscriptions(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"}, turnKey{threadID: "thread-1", turnID: "turn-2"})

	client.events <- Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{ThreadID: "thread-1", TurnID: "turn-2", ItemID: "item-2", Delta: "two"},
	}
	client.events <- Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{ThreadID: "thread-1", TurnID: "turn-1", ItemID: "item-1", Delta: "one"},
	}

	got2 := receiveNotification(t, turn2)
	if params, ok := got2.Params.(AgentMessageDelta); !ok || params.TurnID != "turn-2" || params.Delta != "two" {
		t.Fatalf("turn2 notification = %#v, want turn-2 delta", got2)
	}
	got1 := receiveNotification(t, turn1)
	if params, ok := got1.Params.(AgentMessageDelta); !ok || params.TurnID != "turn-1" || params.Delta != "one" {
		t.Fatalf("turn1 notification = %#v, want turn-1 delta", got1)
	}
	assertNoNotification(t, turn1)
	assertNoNotification(t, turn2)
}

func TestClientSubscribeTurnReplaysBacklog(t *testing.T) {
	client := newRoutingClientForTest(t)

	client.events <- Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{ThreadID: "thread-1", TurnID: "turn-1", ItemID: "item-1", Delta: "early"},
	}
	waitForBacklog(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"}, 1)

	turn, unsubscribe := client.SubscribeTurn(context.Background(), "thread-1", "turn-1")
	defer unsubscribe()

	got := receiveNotification(t, turn)
	if params, ok := got.Params.(AgentMessageDelta); !ok || params.Delta != "early" {
		t.Fatalf("notification = %#v, want replayed early delta", got)
	}
}

func TestClientUnsupportedResponseIDTerminatesClientAndPendingCalls(t *testing.T) {
	conn := newControllableConn()
	client := NewClient(conn)
	t.Cleanup(func() {
		_ = client.Close()
	})
	turn, unsubscribe := client.SubscribeTurn(context.Background(), "thread-1", "turn-1")
	defer unsubscribe()

	callErr := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	safego.Go("codexapp.testPendingCall", func() {
		callErr <- client.Call(ctx, "test/method", nil, nil)
	})
	receiveWrite(t, conn)

	conn.sendLine(`{"id":{},"result":{}}` + "\n")

	err := receiveError(t, callErr)
	if err == nil || !strings.Contains(err.Error(), "unsupported codex app-server response id") {
		t.Fatalf("Call error = %v, want unsupported id client error", err)
	}
	waitForClientDone(t, client)
	assertNotificationChannelClosed(t, turn)
	if err := client.Err(); err == nil || !strings.Contains(err.Error(), "unsupported codex app-server response id") {
		t.Fatalf("Client.Err() = %v, want unsupported id error", err)
	}
}

func TestClientRouterPanicClosesSubscriptionsAndSetsError(t *testing.T) {
	client := newRoutingClientForTest(t)
	turn, unsubscribe := client.SubscribeTurn(context.Background(), "thread-1", "turn-1")
	defer unsubscribe()
	waitForSubscriptions(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"})

	client.routeMu.Lock()
	client.turnBacklog = nil
	client.routeMu.Unlock()

	client.events <- Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{ThreadID: "thread-1", TurnID: "turn-2", ItemID: "item-2", Delta: "panic"},
	}

	select {
	case <-client.routeDone:
	case <-time.After(time.Second):
		t.Fatalf("router did not stop after panic")
	}
	assertNotificationChannelClosed(t, turn)
	if err := client.Err(); err == nil || !strings.Contains(err.Error(), "codex app-server event router panicked") {
		t.Fatalf("Client.Err() = %v, want router panic error", err)
	}
}

func newRoutingClientForTest(t *testing.T) *Client {
	t.Helper()
	client := &Client{
		events:      make(chan Notification, 16),
		done:        make(chan struct{}),
		turnSubs:    map[turnKey]map[chan Notification]struct{}{},
		turnBacklog: map[turnKey][]Notification{},
		routeDone:   make(chan struct{}),
	}
	safego.Go("codexapp.testEventRouter", client.routeEvents)
	t.Cleanup(func() {
		close(client.events)
		select {
		case <-client.routeDone:
		case <-time.After(time.Second):
			t.Fatalf("event router did not stop")
		}
	})
	return client
}

func receiveWrite(t *testing.T, conn *controllableConn) []byte {
	t.Helper()
	select {
	case write := <-conn.writes:
		return write
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for client write")
		return nil
	}
}

func receiveError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for error")
		return nil
	}
}

func waitForClientDone(t *testing.T, client *Client) {
	t.Helper()
	select {
	case <-client.done:
	case <-time.After(time.Second):
		t.Fatalf("client done did not close")
	}
}

func assertNotificationChannelClosed(t *testing.T, ch <-chan Notification) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("subscription delivered notification, want closed")
		}
	case <-time.After(time.Second):
		t.Fatalf("subscription did not close")
	}
}

func receiveNotification(t *testing.T, ch <-chan Notification) Notification {
	t.Helper()
	select {
	case notification, ok := <-ch:
		if !ok {
			t.Fatalf("subscription closed before notification")
		}
		return notification
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for notification")
		return Notification{}
	}
}

func assertNoNotification(t *testing.T, ch <-chan Notification) {
	t.Helper()
	select {
	case notification, ok := <-ch:
		t.Fatalf("unexpected notification %#v, ok=%v", notification, ok)
	case <-time.After(25 * time.Millisecond):
	}
}

func waitForSubscriptions(t *testing.T, client *Client, keys ...turnKey) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if hasSubscriptions(client, keys...) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("subscriptions were not registered")
		case <-ticker.C:
		}
	}
}

func hasSubscriptions(client *Client, keys ...turnKey) bool {
	client.routeMu.Lock()
	defer client.routeMu.Unlock()
	for _, key := range keys {
		if len(client.turnSubs[key]) == 0 {
			return false
		}
	}
	return true
}

func waitForBacklog(t *testing.T, client *Client, key turnKey, count int) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		client.routeMu.Lock()
		got := len(client.turnBacklog[key])
		client.routeMu.Unlock()
		if got == count {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("backlog size = %d, want %d", got, count)
		case <-ticker.C:
		}
	}
}

type panicReadConn struct{}

func (panicReadConn) Read([]byte) (int, error) {
	panic("read boom")
}

func (panicReadConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (panicReadConn) Close() error {
	return nil
}

type controllableConn struct {
	mu        sync.Mutex
	readBuf   []byte
	reads     chan []byte
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newControllableConn() *controllableConn {
	return &controllableConn{
		reads:  make(chan []byte, 16),
		writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (c *controllableConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if len(c.readBuf) > 0 {
			n := copy(p, c.readBuf)
			c.readBuf = c.readBuf[n:]
			c.mu.Unlock()
			return n, nil
		}
		c.mu.Unlock()

		select {
		case data := <-c.reads:
			if data == nil {
				return 0, io.EOF
			}
			c.mu.Lock()
			c.readBuf = append(c.readBuf, data...)
			c.mu.Unlock()
		case <-c.closed:
			return 0, io.ErrClosedPipe
		}
	}
}

func (c *controllableConn) Write(p []byte) (int, error) {
	copied := append([]byte(nil), p...)
	select {
	case c.writes <- copied:
	case <-c.closed:
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func (c *controllableConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return nil
}

func (c *controllableConn) sendLine(line string) {
	c.reads <- []byte(line)
}

var _ Conn = (*controllableConn)(nil)
