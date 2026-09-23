package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type deliveryTestAPI struct {
	runRenderBotAPI
	blockedChat    int64
	chatError      error
	attemptedChats []int64
	guests         []AnswerGuestQueryRequest
	documents      []SendDocumentRequest
	cancel         context.CancelFunc
}

func (a *deliveryTestAPI) SendMessage(ctx context.Context, req SendMessageRequest) (SentMessage, error) {
	a.attemptedChats = append(a.attemptedChats, req.ChatID)
	if a.cancel != nil {
		a.cancel()
		return SentMessage{}, ctx.Err()
	}
	if req.ChatID == a.blockedChat && a.chatError != nil {
		return SentMessage{}, a.chatError
	}
	return a.runRenderBotAPI.SendMessage(ctx, req)
}

func (a *deliveryTestAPI) AnswerGuestQuery(_ context.Context, req AnswerGuestQueryRequest) (SentGuestMessage, error) {
	a.guests = append(a.guests, req)
	return SentGuestMessage{InlineMessageID: "guest-result"}, nil
}

func (a *deliveryTestAPI) SendDocument(_ context.Context, req SendDocumentRequest) (SentMessage, error) {
	a.documents = append(a.documents, req)
	return SentMessage{MessageID: 99}, nil
}

type deliveryTestDaemon struct {
	mu           sync.Mutex
	deliveries   []core.ClientDelivery
	runErrors    map[string]int
	ackFailures  int
	ackAttempts  int
	acked        map[string]bool
	failed       []string
	storageError int
	storageReads int
}

func (d *deliveryTestDaemon) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	switch {
	case r.URL.Path == "/v1/client-deliveries":
		pending := []core.ClientDelivery{}
		for _, delivery := range d.deliveries {
			if !d.acked[delivery.ID] && delivery.Type == r.URL.Query().Get("type") {
				pending = append(pending, delivery)
			}
		}
		write(core.ClientDeliveriesResponse{Deliveries: pending})
	case strings.HasPrefix(r.URL.Path, "/v1/runs/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
		if status := d.runErrors[id]; status != 0 {
			w.WriteHeader(status)
			write(map[string]string{"error": "daemon unavailable"})
			return
		}
		write(core.RunResponse{Run: core.Run{ID: id, Status: core.RunStatusCompleted}})
	case r.URL.Path == "/v1/messages":
		id := r.URL.Query().Get("session_id")
		write(core.MessagesResponse{Messages: []transcript.Message{
			{ID: "commentary-" + id, RunID: id, Role: transcript.MessageRoleAssistant, Content: "Let me check."},
			{ID: "final-" + id, RunID: id, Role: transcript.MessageRoleAssistant, Content: "Final answer."},
			{ID: "different-run", RunID: "unrelated", Role: transcript.MessageRoleAssistant, Content: "Ignore this."},
		}})
	case strings.HasSuffix(r.URL.Path, "/ack"):
		d.ackAttempts++
		if d.ackFailures > 0 {
			d.ackFailures--
			w.WriteHeader(http.StatusServiceUnavailable)
			write(map[string]string{"error": "ack unavailable"})
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/client-deliveries/"), "/ack")
		if d.acked == nil {
			d.acked = map[string]bool{}
		}
		d.acked[id] = true
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(r.URL.Path, "/fail"):
		d.failed = append(d.failed, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(r.URL.Path, "/v1/modules/storage/files/"):
		d.storageReads++
		if d.storageError != 0 {
			w.WriteHeader(d.storageError)
			write(map[string]string{"error": "storage unavailable"})
			return
		}
		write(map[string]any{"file": map[string]string{"path": "result.txt", "title": "result.txt", "mime_type": "text/plain"}, "content_base64": "aGVsbG8="})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newDeliveryTestWorker(t *testing.T, daemon *deliveryTestDaemon, api *deliveryTestAPI, now *time.Time) *Worker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(daemon.serve))
	t.Cleanup(server.Close)
	return &Worker{api: api, config: Config{BaseURL: server.URL, DaemonHTTPClient: server.Client(), ClientName: "telegram-test"}, now: func() time.Time { return *now }}
}

func testRunDelivery(id string, address DeliveryAddress) core.ClientDelivery {
	return core.ClientDelivery{ID: id, Type: core.ClientDeliveryTypeRun, RunID: id, SessionID: id, Address: encodeDeliveryAddress(address)}
}

func TestDeliveryFloodWaitDoesNotBlockOtherChats(t *testing.T) {
	now := time.Unix(100, 0)
	d := &deliveryTestDaemon{deliveries: []core.ClientDelivery{
		testRunDelivery("blocked", DeliveryAddress{ChatID: 1}),
		testRunDelivery("same-chat", DeliveryAddress{ChatID: 1}),
		testRunDelivery("other", DeliveryAddress{ChatID: 2}),
	}}
	api := &deliveryTestAPI{blockedChat: 1, chatError: &APIError{ErrorCode: 429, RetryAfter: time.Minute}}
	w := newDeliveryTestWorker(t, d, api, &now)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.deliverPendingRuns(ctx); !IsRetryable(err) {
		t.Fatalf("expected retryable flood error, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("delivery slept through Telegram cooldown")
	}
	if len(api.attemptedChats) != 3 || api.attemptedChats[0] != 1 || api.attemptedChats[1] != 2 {
		t.Fatalf("attempted chats=%v, want one failed attempt then two chunks in other chat", api.attemptedChats)
	}
	now = now.Add(30 * time.Second)
	if err := w.deliverPendingRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.attemptedChats) != 3 {
		t.Fatal("retried before retry_after")
	}
	now = now.Add(30 * time.Second)
	api.chatError = nil
	if err := w.deliverPendingRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.acked) != 3 || len(d.failed) != 0 {
		t.Fatalf("acked=%v failed=%v", d.acked, d.failed)
	}
}

func TestDeliveryRetriesDaemonFailureWithoutMarkingFailed(t *testing.T) {
	now := time.Unix(100, 0)
	d := &deliveryTestDaemon{deliveries: []core.ClientDelivery{
		testRunDelivery("unavailable", DeliveryAddress{ChatID: 1}),
		testRunDelivery("other", DeliveryAddress{ChatID: 2}),
	}, runErrors: map[string]int{"unavailable": http.StatusServiceUnavailable}}
	w := newDeliveryTestWorker(t, d, &deliveryTestAPI{}, &now)
	if err := w.deliverPendingRuns(context.Background()); !retryableDeliveryError(err) {
		t.Fatalf("error=%v", err)
	}
	d.mu.Lock()
	if len(d.failed) != 0 || !d.acked["other"] {
		t.Fatalf("failed=%v acked=%v", d.failed, d.acked)
	}
	delete(d.runErrors, "unavailable")
	d.mu.Unlock()
	now = now.Add(3 * time.Second)
	if err := w.deliverPendingRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.acked["unavailable"] {
		t.Fatal("transient daemon failure lost the delivery")
	}
}

func TestFinalDeliveryUsesLatestAnswerAndRetriesOnlyAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address DeliveryAddress
	}{
		{"chat", DeliveryAddress{ChatID: 1}},
		{"inline", DeliveryAddress{Kind: telegramTargetInline, InlineMessageID: "inline-1"}},
		{"guest", DeliveryAddress{Kind: telegramTargetGuest, GuestQueryID: "guest-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			d := &deliveryTestDaemon{deliveries: []core.ClientDelivery{testRunDelivery("run", tc.address)}, ackFailures: 1}
			api := &deliveryTestAPI{}
			w := newDeliveryTestWorker(t, d, api, &now)
			if err := w.deliverPendingRuns(context.Background()); !retryableDeliveryError(err) {
				t.Fatalf("ack error=%v", err)
			}
			if tc.name == "inline" && (len(api.edits) != 1 || api.edits[0].Text != "Final answer.") {
				t.Fatalf("inline edits=%+v", api.edits)
			}
			if tc.name == "guest" && (len(api.guests) != 1 || api.guests[0].Result.InputMessageContent.MessageText != "Final answer.") {
				t.Fatalf("guest answers=%+v", api.guests)
			}
			before := len(api.messages) + len(api.edits) + len(api.guests)
			now = now.Add(3 * time.Second)
			if err := w.deliverPendingRuns(context.Background()); err != nil {
				t.Fatal(err)
			}
			if after := len(api.messages) + len(api.edits) + len(api.guests); after != before {
				t.Fatalf("delivery resent after ack failure: %d -> %d", before, after)
			}
			if len(w.deliveryReceipts) != 0 || len(w.states) != 0 {
				t.Fatal("successful acknowledgement leaked delivery state")
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.ackAttempts != 2 || len(d.failed) != 0 || !d.acked["run"] {
				t.Fatalf("daemon state: attempts=%d failed=%v acked=%v", d.ackAttempts, d.failed, d.acked)
			}
		})
	}
}

func TestDocumentRetriesStorageAndAcknowledgementWithoutResending(t *testing.T) {
	now := time.Unix(100, 0)
	d := &deliveryTestDaemon{deliveries: []core.ClientDelivery{{ID: "doc", Type: core.ClientDeliveryTypeDocument, Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 1}), Payload: json.RawMessage(`{"storage_path":"result.txt"}`)}}, storageError: http.StatusServiceUnavailable, ackFailures: 1}
	api := &deliveryTestAPI{}
	w := newDeliveryTestWorker(t, d, api, &now)
	if err := w.deliverPendingDocuments(context.Background()); !retryableDeliveryError(err) {
		t.Fatalf("storage error=%v", err)
	}
	d.mu.Lock()
	if len(d.failed) != 0 {
		t.Fatal("transient storage error permanently failed document")
	}
	d.storageError = 0
	d.mu.Unlock()
	now = now.Add(3 * time.Second)
	if err := w.deliverPendingDocuments(context.Background()); !retryableDeliveryError(err) {
		t.Fatalf("ack error=%v", err)
	}
	now = now.Add(3 * time.Second)
	if err := w.deliverPendingDocuments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.documents) != 1 || string(api.documents[0].Document) != "hello" {
		t.Fatalf("documents=%+v", api.documents)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.storageReads != 2 || !d.acked["doc"] || len(d.failed) != 0 {
		t.Fatalf("storage reads=%d acked=%v failed=%v", d.storageReads, d.acked, d.failed)
	}
}

func TestDeliveryCancellationLeavesPendingWork(t *testing.T) {
	now := time.Unix(100, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &deliveryTestDaemon{deliveries: []core.ClientDelivery{testRunDelivery("cancelled", DeliveryAddress{ChatID: 1})}}
	w := newDeliveryTestWorker(t, d, &deliveryTestAPI{cancel: cancel}, &now)
	if err := w.deliverPendingRuns(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.acked) != 0 || len(d.failed) != 0 {
		t.Fatalf("cancelled work finalized: acked=%v failed=%v", d.acked, d.failed)
	}
}
