package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// fakeDaemon serves the binding, the snapshot and an event stream that stays
// open; open counts the streams currently connected.
func fakeDaemon(t *testing.T, snapshot core.ClientSnapshot) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var open atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(core.ClientBindingResponse{Binding: core.ClientBinding{SessionID: snapshot.SessionID}})
		case "/v1/snapshot":
			_ = json.NewEncoder(w).Encode(core.ClientSnapshotResponse{Snapshot: snapshot})
		case "/v1/events":
			open.Add(1)
			defer open.Add(-1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: ready\ndata: {}\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &open
}

// run executes cmd and feeds the connect and snapshot messages it produces back
// into the app; commands still waiting after 300ms (live events) are left.
func run(m *appModel, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-result:
	case <-time.After(300 * time.Millisecond):
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, inner := range batch {
			run(m, inner)
		}
		return
	}
	switch msg.(type) {
	case connectedMsg, snapshotMsg:
		_, next := m.Update(msg)
		run(m, next)
	}
}

func TestReloadingClosesThePreviousEventStream(t *testing.T) {
	server, open := fakeDaemon(t, core.ClientSnapshot{SessionID: "session_1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newApp(ctx, New(Config{BaseURL: server.URL}))

	for range 3 {
		run(m, m.reload())
	}

	deadline := time.Now().Add(2 * time.Second)
	for open.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := open.Load(); got != 1 {
		t.Fatalf("open event streams = %d, want 1", got)
	}
	if m.read == nil || m.read.SessionID() != "session_1" || !m.stream.loaded {
		t.Fatal("the snapshot was not loaded after subscribing")
	}
}

func liveMessage(t *testing.T, id uint64, eventType core.EventType, message transcript.Message) daemonclient.LiveEvent {
	t.Helper()
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return daemonclient.LiveEvent{ID: id, Type: eventType, SessionID: "session_1", Payload: raw}
}

func TestEventsAroundTheSnapshotAreAppliedExactlyOnce(t *testing.T) {
	m := newApp(context.Background(), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	id := m.stream.id
	m.Update(connectedMsg{streamID: id, sessionID: "session_1"})

	// Before the snapshot: one event it already covers, one it does not.
	m.Update(liveEventMsg{streamID: id, event: liveMessage(t, 5, core.EventMessageUpdated, textMessage("m1", transcript.MessageRoleAssistant, 1, "draft"))})
	m.Update(liveEventMsg{streamID: id, event: liveMessage(t, 7, core.EventMessageCreated, textMessage("m2", transcript.MessageRoleUser, 2, "next question"))})
	m.Update(snapshotMsg{streamID: id, snapshot: core.ClientSnapshot{EventID: 6, SessionID: "session_1", Messages: []transcript.Message{
		textMessage("m1", transcript.MessageRoleAssistant, 1, "final answer"),
	}}})
	// A late copy of a covered event, then a new one.
	m.Update(liveEventMsg{streamID: id, event: liveMessage(t, 6, core.EventMessageUpdated, textMessage("m1", transcript.MessageRoleAssistant, 1, "stale"))})
	m.Update(liveEventMsg{streamID: id, event: liveMessage(t, 8, core.EventMessageCreated, textMessage("m3", transcript.MessageRoleAssistant, 3, "reply"))})

	messages := m.read.Messages()
	if len(messages) != 3 || messages[0].Content().Text != "final answer" || messages[1].ID != "m2" || messages[2].ID != "m3" {
		got := make([]string, 0, len(messages))
		for _, message := range messages {
			got = append(got, message.ID+"="+message.Content().Text)
		}
		t.Fatalf("messages = %v", got)
	}
}

func TestARunIsFollowedFromLiveEventsWithoutReloading(t *testing.T) {
	m, _ := renderApp(t, 100, 30, core.ClientSnapshot{SessionID: "session_1", Session: renderSession(), Context: renderContext()})
	stream := m.stream.id
	running := core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusRunning, StartedAt: at(0)}

	if _, cmd := m.Update(sendMessageResultMsg{content: "hello", result: core.AcceptRunResult{SessionID: "session_1", Status: core.AcceptRunStatusStarted, Run: running}}); cmd != nil {
		t.Fatal("a sent message asked for more than the live events")
	}
	deliver(t, m, core.EventMessageCreated, textMessage("m1", transcript.MessageRoleUser, 0, "hello"))
	deliver(t, m, core.EventRunUpdated, running)
	deliver(t, m, core.EventMessageCreated, finishedReply("m2", 5, "Hi there."))
	if !m.busy {
		t.Fatal("not busy while the run runs")
	}
	completed := running
	completed.Status = core.RunStatusCompleted
	deliver(t, m, core.EventRunUpdated, completed)

	if m.busy || m.stream.id != stream || m.loading {
		t.Fatalf("after the run: busy=%v reloaded=%v loading=%v", m.busy, m.stream.id != stream, m.loading)
	}
	screen := ansi.Strip(m.viewContent())
	for _, want := range []string{"hello", "Hi there."} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
}
