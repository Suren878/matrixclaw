package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runDaemon serves one run delivery to chat 7 and lists the run's messages the
// way the store does; tests append and update messages between deliveries.
type runDaemon struct {
	mu        sync.Mutex
	run       core.Run
	progress  core.RunProgress
	messages  []transcript.Message
	approvals []core.Approval
	served    int
	acked     bool
}

func newRunDaemon() *runDaemon {
	return &runDaemon{run: core.Run{ID: "run-1", SessionID: "s1", Status: core.RunStatusRunning}}
}

func (d *runDaemon) add(messages ...transcript.Message) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, message := range messages {
		message.SessionID = d.run.SessionID
		if message.RunID == "" {
			message.RunID = d.run.ID
		}
		message.Seq = int64(len(d.messages) + 1)
		d.messages = append(d.messages, message)
	}
}

func (d *runDaemon) update(message transcript.Message) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.messages {
		if existing.ID == message.ID {
			message.SessionID, message.RunID, message.Seq = existing.SessionID, existing.RunID, existing.Seq
			d.messages[i] = message
		}
	}
}

func (d *runDaemon) set(update func(*runDaemon)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	update(d)
}

func (d *runDaemon) servedRows() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.served
}

func (d *runDaemon) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	query := r.URL.Query()
	switch {
	case r.URL.Path == "/v1/client-deliveries":
		deliveries := []core.ClientDelivery{}
		if !d.acked && query.Get("type") == core.ClientDeliveryTypeRun {
			deliveries = append(deliveries, core.ClientDelivery{ID: "delivery-1", Type: core.ClientDeliveryTypeRun, RunID: d.run.ID, SessionID: d.run.SessionID, Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 7})})
		}
		write(core.ClientDeliveriesResponse{Deliveries: deliveries})
	case r.URL.Path == "/v1/runs/"+d.run.ID+"/progress":
		write(core.RunProgressResponse{Progress: d.progress})
	case r.URL.Path == "/v1/runs/"+d.run.ID:
		write(core.RunResponse{Run: d.run})
	case r.URL.Path == "/v1/approvals":
		write(core.ApprovalsResponse{Approvals: d.approvals})
	case r.URL.Path == "/v1/messages":
		limit, _ := strconv.Atoi(query.Get("limit"))
		var listed []transcript.Message
		if raw := query.Get("after_seq"); raw != "" {
			after, _ := strconv.ParseInt(raw, 10, 64)
			for _, message := range d.messages {
				if message.Seq > after && (limit == 0 || len(listed) < limit) {
					listed = append(listed, message)
				}
			}
		} else {
			listed = d.messages
			if limit > 0 && len(listed) > limit {
				listed = listed[len(listed)-limit:]
			}
		}
		d.served += len(listed)
		write(core.MessagesResponse{Messages: listed})
	case strings.HasSuffix(r.URL.Path, "/ack"):
		d.acked = true
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newRunDaemonWorker(t *testing.T, d *runDaemon, api BotAPI, now *time.Time) *Worker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(server.Close)
	return &Worker{api: api, config: Config{BaseURL: server.URL, DaemonHTTPClient: server.Client(), ClientName: "telegram-test"}, now: func() time.Time { return *now }}
}

func toolCallMessage(id string, name string, input string, finished bool) transcript.Message {
	return transcript.Message{ID: id, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: name, Input: input, Finished: finished},
	}}}
}

func toolResultMessage(callID string, name string) transcript.Message {
	return transcript.Message{ID: "result-" + callID, Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: callID, Name: name, Content: "ok"},
	}}}
}

func finalReply(text string) transcript.Message {
	return transcript.Message{ID: "final", Role: transcript.MessageRoleAssistant, Content: text, Parts: []transcript.MessagePart{
		{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: text}},
		{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "stop"}},
	}}
}

func TestLongRunDeliveryReadsEachMessageAboutOnce(t *testing.T) {
	now := time.Unix(100, 0)
	d := newRunDaemon()
	api := &runRenderBotAPI{}
	w := newRunDaemonWorker(t, d, api, &now)
	deliver := func() {
		t.Helper()
		now = now.Add(time.Second)
		if err := w.deliverPendingRuns(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	d.add(transcript.Message{ID: "user", Role: transcript.MessageRoleUser, Content: "Fix everything"})

	const steps = 200
	for i := range steps {
		id := fmt.Sprintf("call-%d", i)
		d.add(toolCallMessage(id, "bash", `{"command":"go test ./..."}`, false))
		deliver()
		d.update(toolCallMessage(id, "bash", `{"command":"go test ./..."}`, true))
		d.add(toolResultMessage(id, "bash"))
		deliver()
	}
	d.add(finalReply("All fixed."))
	d.set(func(d *runDaemon) { d.run.Status = core.RunStatusCompleted })
	deliver()

	if !d.acked {
		t.Fatal("final delivery was not acknowledged")
	}
	if texts := api.messageTexts(); texts[len(texts)-1] != "All fixed." {
		t.Fatalf("last message = %q", texts[len(texts)-1])
	}
	total := 2 + 2*steps
	if served := d.servedRows(); served > 4*total {
		t.Fatalf("message rows read = %d for %d messages, want O(messages)", served, total)
	}
}

func TestRunMessagesSeeUpdatesOfMessagesThatMayStillChange(t *testing.T) {
	now := time.Unix(100, 0)
	d := newRunDaemon()
	w := newRunDaemonWorker(t, d, &runRenderBotAPI{}, &now)
	state := newRunDeliveryState()
	load := func() []transcript.Message {
		t.Helper()
		messages, err := w.runMessages(context.Background(), w.daemon(""), "s1", "run-1", state)
		if err != nil {
			t.Fatal(err)
		}
		return messages
	}
	for i := range runMessagesFirstLoad + 50 {
		d.add(transcript.Message{ID: fmt.Sprintf("old-%d", i), RunID: "run-0", Role: transcript.MessageRoleUser, Content: "earlier"})
	}
	streaming := transcript.Message{ID: "reply", Role: transcript.MessageRoleAssistant, Content: "Let me"}
	d.add(streaming, toolCallMessage("call-1", "bash", `{}`, false))

	if got := load(); len(got) != 2 || d.servedRows() != runMessagesFirstLoad {
		t.Fatalf("first load: %d run messages, %d rows read", len(got), d.servedRows())
	}
	streaming.Content = "Let me check."
	d.update(streaming)
	d.add(toolResultMessage("call-1", "bash"), transcript.Message{ID: "other", RunID: "run-2", Role: transcript.MessageRoleUser})
	got := load()
	if len(got) != 3 || got[0].Content != "Let me check." || got[2].ID != "result-call-1" {
		t.Fatalf("run messages = %+v", got)
	}
}
