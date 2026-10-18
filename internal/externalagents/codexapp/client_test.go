package codexapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestClientSubscribeTurnRoutesInterleavedEvents(t *testing.T) {
	client := newRoutingClientForTest()
	turn1 := client.SubscribeTurn("thread-1", "turn-1")
	defer turn1.Close()
	turn2 := client.SubscribeTurn("thread-1", "turn-2")
	defer turn2.Close()

	client.route(turnDeltaNotification(turnKey{threadID: "thread-1", turnID: "turn-2"}, "two"))
	client.route(turnDeltaNotification(turnKey{threadID: "thread-1", turnID: "turn-1"}, "one"))

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

func TestClientSubscribeTurnReplaysBacklogAndCoalescesBursts(t *testing.T) {
	for _, subscribeFirst := range []bool{false, true} {
		client := newRoutingClientForTest()
		key := turnKey{threadID: "thread-1", turnID: "turn-1"}
		var turn *TurnSubscription
		if subscribeFirst {
			turn = client.SubscribeTurn(key.threadID, key.turnID)
		}
		const deltaCount = 2048
		var want strings.Builder
		for i := range deltaCount {
			delta := fmt.Sprintf("d%04d", i)
			want.WriteString(delta)
			client.route(turnDeltaNotification(key, delta))
		}
		client.route(Notification{Method: "turn/completed", Params: TurnCompleted{ThreadID: key.threadID, Turn: Turn{ID: key.turnID, Status: TurnStatusCompleted}}})
		if !subscribeFirst {
			turn = client.SubscribeTurn(key.threadID, key.turnID)
		}
		var text strings.Builder
		for terminal := false; !terminal; {
			switch params := receiveNotification(t, turn).Params.(type) {
			case AgentMessageDelta:
				text.WriteString(params.Delta)
			case TurnCompleted:
				terminal = true
			}
		}
		if text.String() != want.String() {
			t.Fatalf("subscribe first %t: combined delta = %d bytes, want %d", subscribeFirst, text.Len(), want.Len())
		}
		assertNoNotification(t, turn)
		turn.Close()
	}
}

func TestClientDropsBacklogOfTurnsNobodySubscribed(t *testing.T) {
	client := newRoutingClientForTest()
	now := time.Now()
	client.now = func() time.Time { return now }
	finished := turnKey{threadID: "thread-1", turnID: "finished"}
	client.route(turnDeltaNotification(finished, "late"))
	client.route(Notification{Method: "turn/completed", Params: TurnCompleted{ThreadID: finished.threadID, Turn: Turn{ID: finished.turnID}}})

	now = now.Add(turnBacklogTTL + time.Second)
	client.route(turnDeltaNotification(turnKey{threadID: "thread-1", turnID: "next"}, "x"))

	client.routeMu.Lock()
	_, kept := client.turnBacklog[finished]
	backlogs := len(client.turnBacklog)
	client.routeMu.Unlock()
	if kept || backlogs != 1 {
		t.Fatalf("backlog kept finished turn = %t, backlogs = %d; want only the new turn", kept, backlogs)
	}
}

func TestClientWriteToStalledServerFailsInTime(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := client.Notify(ctx, "initialized", nil); err == nil {
		t.Fatal("Notify() to a server that never reads succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Notify() took %v, want about the context deadline", elapsed)
	}
	if client.Err() == nil {
		t.Fatal("a failed write left the connection usable")
	}
}

func TestRuntimeFailsTurnWhoseEndCannotBeDecoded(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn)
	runtime := NewRuntime(RuntimeOptions{Enabled: true, Client: client})
	t.Cleanup(func() {
		_ = runtime.Close()
		_ = client.Close()
		_ = serverConn.Close()
	})
	go func() {
		decoder := json.NewDecoder(serverConn)
		for {
			var request map[string]json.RawMessage
			if decoder.Decode(&request) != nil {
				return
			}
			id := string(request["id"])
			switch string(request["method"]) {
			case `"initialize"`:
				_, _ = serverConn.Write([]byte(`{"id":` + id + `,"result":{}}` + "\n"))
			case `"turn/start"`:
				_, _ = serverConn.Write([]byte(`{"id":` + id + `,"result":{"turn":{"id":"turn-1"}}}` + "\n"))
				_, _ = serverConn.Write([]byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":{"kind":"completed"}}}}` + "\n"))
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, err := runtime.Send(ctx, externalagents.ExternalSession{ExternalThreadID: "thread-1"}, externalagents.Input{Text: "hi"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	var last externalagents.Event
	for event := range events {
		last = event
	}
	if last.Kind != externalagents.EventTurnFailed || !strings.Contains(last.Error, "turn/completed") {
		t.Fatalf("last event = %#v, want a failed turn naming turn/completed", last)
	}
}

func newRoutingClientForTest() *Client {
	return &Client{
		done:        make(chan struct{}),
		turnSubs:    map[turnKey]*TurnSubscription{},
		turnBacklog: map[turnKey]*turnBacklog{},
		now:         time.Now,
	}
}

func turnDeltaNotification(key turnKey, delta string) Notification {
	return Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{
			ThreadID: key.threadID,
			TurnID:   key.turnID,
			ItemID:   "item-1",
			Delta:    delta,
		},
	}
}

func receiveNotification(t *testing.T, sub *TurnSubscription) Notification {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notification, ok := sub.Next(ctx)
	if !ok {
		t.Fatalf("no notification")
	}
	return notification
}

func assertNoNotification(t *testing.T, sub *TurnSubscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if notification, ok := sub.Next(ctx); ok {
		t.Fatalf("unexpected notification %#v", notification)
	}
}

func TestClientAnswersServerRequestsWithoutTakingTheirIDs(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})

	answers := make(chan map[string]json.RawMessage, 4)
	go func() {
		decoder := json.NewDecoder(serverConn)
		var request map[string]json.RawMessage
		if decoder.Decode(&request) != nil {
			return
		}
		id := string(request["id"])
		_, _ = serverConn.Write([]byte(`{"id":` + id + `,"method":"item/commandExecution/requestApproval","params":{"threadId":"t","turnId":"u","itemId":"i","command":"rm -rf /"}}` + "\n"))
		_, _ = serverConn.Write([]byte(`{"id":"fc","method":"item/fileChange/requestApproval","params":{"threadId":"t","turnId":"u","itemId":"j"}}` + "\n"))
		_, _ = serverConn.Write([]byte(`{"id":"x","method":"item/tool/call","params":{}}` + "\n"))
		_, _ = serverConn.Write([]byte(`{"id":` + id + `,"result":{"turn":{"id":"real-turn"}}}` + "\n"))
		for range 3 {
			var answer map[string]json.RawMessage
			if decoder.Decode(&answer) != nil {
				return
			}
			answers <- answer
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.StartTurn(ctx, TurnStartParams{ThreadID: "t"})
	if err != nil || resp.Turn.ID != "real-turn" {
		t.Fatalf("StartTurn() = %#v, %v; want real-turn", resp, err)
	}
	byID := map[string]string{}
	for range 3 {
		select {
		case answer := <-answers:
			byID[string(answer["id"])] = string(answer["result"]) + string(answer["error"])
		case <-ctx.Done():
			t.Fatalf("server requests answered = %v, want 3", byID)
		}
	}
	if got := byID[`"fc"`]; got != `{"decision":"decline"}` {
		t.Fatalf("file change answer = %s, want decline", got)
	}
	if got := byID[`"x"`]; !strings.Contains(got, `"code":-32601`) {
		t.Fatalf("unsupported request answer = %s, want method-not-found error", got)
	}
	for id, got := range byID {
		if id != `"fc"` && id != `"x"` && got != `{"decision":"decline"}` {
			t.Fatalf("command approval answer = %s, want decline", got)
		}
	}
}
