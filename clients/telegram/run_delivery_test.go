package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	canceled  int
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
		if !d.acked && (query.Get("type") == "" || query.Get("type") == core.ClientDeliveryTypeRun) {
			deliveries = append(deliveries, core.ClientDelivery{ID: "delivery-1", Type: core.ClientDeliveryTypeRun, RunID: d.run.ID, SessionID: d.run.SessionID, Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 7})})
		}
		write(core.ClientDeliveriesResponse{Deliveries: deliveries})
	case r.URL.Path == "/v1/runs/"+d.run.ID+"/cancel":
		d.canceled++
		if !runFinished(d.run.Status) {
			d.run.Status = core.RunStatusCanceled
		}
		write(core.RunResponse{Run: d.run})
	case r.URL.Path == "/v1/snapshot":
		run := d.run
		write(core.ClientSnapshotResponse{Snapshot: core.ClientSnapshot{SessionID: run.SessionID, Run: &run}})
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
	worker := newWorker(Config{BaseURL: server.URL}, api)
	worker.now = func() time.Time { return *now }
	return worker
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
		if err := w.deliverPending(context.Background()); err != nil {
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
	if texts := api.messageTexts(); len(texts) != 2 || texts[1] != "All fixed." {
		t.Fatalf("messages = %q, want the run status and the answer", texts)
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

func TestARestartedWorkerContinuesTheRunsMessages(t *testing.T) {
	now := time.Now()
	d := newRunDaemon()
	api := &runRenderBotAPI{}
	server := httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(server.Close)
	config := Config{BaseURL: server.URL, RenderStatePath: filepath.Join(t.TempDir(), "render.json")}
	deliver := func(w *Worker) {
		t.Helper()
		now = now.Add(5 * time.Second)
		w.now = func() time.Time { return now }
		if err := w.deliverPending(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	segment := transcript.Message{ID: "seg-1", Role: transcript.MessageRoleAssistant, Content: "Let me look at the logs first.", Parts: []transcript.MessagePart{
		{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: "Let me look at the logs first."}},
		{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}},
	}}
	d.add(transcript.Message{ID: "user", Role: transcript.MessageRoleUser, Content: "Why is it down?"}, segment, toolCallMessage("call-1", "bash", `{"command":"journalctl"}`, false))
	deliver(newWorker(config, api))

	d.add(toolResultMessage("call-1", "bash"), finalReply("Done."))
	d.set(func(d *runDaemon) { d.run.Status = core.RunStatusCompleted })
	deliver(newWorker(config, api))

	for _, edit := range api.edits {
		if edit.MessageID == 2 {
			t.Fatalf("the shown segment was edited again: %+v", edit)
		}
	}
	texts := api.messageTexts()
	if len(texts) != 3 || !strings.HasPrefix(texts[0], "✅ Done") || texts[1] != "Let me look at the logs first." || texts[2] != "Done." {
		t.Fatalf("messages = %q, want the status edited to done, the segment once and the answer", texts)
	}
}

func TestTheRunStatusMessageCancelsTheRun(t *testing.T) {
	now := time.Unix(100, 0)
	d := newRunDaemon()
	api := &runRenderBotAPI{}
	w := newRunDaemonWorker(t, d, api, &now)
	d.add(transcript.Message{ID: "user", Role: transcript.MessageRoleUser, Content: "Deploy"}, toolCallMessage("call-1", "bash", `{"command":"sleep 600"}`, false))
	if err := w.deliverPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	markup, _ := api.messages[0].ReplyMarkup.(*InlineKeyboardMarkup)
	if markup == nil || markup.InlineKeyboard[0][0].CallbackData != cbCancelRun+"run-1" {
		t.Fatalf("status message markup = %#v", api.messages[0].ReplyMarkup)
	}

	now = now.Add(5 * time.Second)
	tap := &CallbackQuery{ID: "cq", From: &User{ID: 7}, Message: &Message{MessageID: 1, Chat: Chat{ID: 7, Type: "private"}}, Data: markup.InlineKeyboard[0][0].CallbackData}
	if err := w.handleCallbackQuery(context.Background(), tap); err != nil {
		t.Fatal(err)
	}

	if d.canceled != 1 || !d.acked {
		t.Fatalf("canceled = %d, delivery acked = %v", d.canceled, d.acked)
	}
	last := api.edits[len(api.edits)-1]
	if last.MessageID != 1 || !strings.HasPrefix(last.Text, "⛔ Canceled") || last.ReplyMarkup != nil {
		t.Fatalf("final status edit = %+v", last)
	}
}

func TestCancelCommandCancelsTheSessionsRun(t *testing.T) {
	now := time.Unix(100, 0)
	d := newRunDaemon()
	d.set(func(d *runDaemon) { d.acked = true }) // the run is delivered elsewhere
	api := &runRenderBotAPI{}
	w := newRunDaemonWorker(t, d, api, &now)
	cancel := func() {
		t.Helper()
		if err := w.handleTextMessage(context.Background(), &Message{MessageID: int64(len(api.messages) + 100), Chat: Chat{ID: 7, Type: "private"}, From: &User{ID: 7}, Text: "/cancel"}); err != nil {
			t.Fatal(err)
		}
	}

	cancel()
	cancel()

	if texts := api.messageTexts(); d.canceled != 1 || len(texts) != 2 || texts[0] != "Run canceled." || texts[1] != "Nothing is running." {
		t.Fatalf("canceled = %d, replies = %q", d.canceled, texts)
	}
}
