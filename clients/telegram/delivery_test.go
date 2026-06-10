package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestDeliverPendingSessionRunRendersCompletedRunMessagesAndAcknowledges(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	acked := make(chan struct{})
	var ackOnce sync.Once
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			if got := r.URL.Query().Get("type"); got != core.ClientDeliveryTypeRun {
				t.Errorf("delivery type query = %q, want %s", got, core.ClientDeliveryTypeRun)
			}
			if got := r.URL.Query().Get("status"); got != string(core.ClientDeliveryStatusPending) {
				t.Errorf("delivery status query = %q, want pending", got)
			}
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_session_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "123",
				SessionID:   "session_1",
				RunID:       "run_1",
				Address:     json.RawMessage(`{"chat_id":123,"thread_id":7}`),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_1":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_1",
				SessionID: "session_1",
				Status:    core.RunStatusCompleted,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			if got := r.URL.Query().Get("session_id"); got != "session_1" {
				t.Errorf("messages session_id = %q, want session_1", got)
			}
			if got := r.URL.Query().Get("limit"); got != "0" {
				t.Errorf("messages limit = %q, want 0", got)
			}
			writeJSON(t, w, core.MessagesResponse{
				Messages: []core.Message{{
					ID:        "assistant_1",
					SessionID: "session_1",
					RunID:     "run_1",
					Role:      core.MessageRoleAssistant,
					Content:   "Delivered after restart.",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/client-deliveries/delivery_session_1/ack":
			ackOnce.Do(func() { close(acked) })
			writeJSON(t, w, core.OKResponse{OK: true})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:             daemon.URL,
			ClientName:          "telegram",
			DaemonHTTPClient:    daemon.Client(),
			StreamFlushInterval: time.Hour,
			PollRetryDelay:      time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}

	select {
	case <-acked:
	case <-ctx.Done():
		t.Fatalf("delivery was not acknowledged: %v", ctx.Err())
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.messages) != 1 {
		t.Fatalf("messages = %#v, want one assistant message", fakeAPI.messages)
	}
	if fakeAPI.messages[0].ChatID != 123 {
		t.Fatalf("message chat = %d, want main chat 123", fakeAPI.messages[0].ChatID)
	}
	if !strings.Contains(fakeAPI.messages[0].Text, "Delivered after restart.") {
		t.Fatalf("message text = %q, want assistant snapshot content", fakeAPI.messages[0].Text)
	}
}

func TestDeliverPendingSessionRunSendsAssistantAnswerAsSingleMessage(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_session_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "123",
				SessionID:   "session_1",
				RunID:       "run_1",
				Address:     encodeDeliveryAddress(DeliveryAddress{ChatID: 123}),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_1":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_1",
				SessionID: "session_1",
				Status:    core.RunStatusCompleted,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			writeJSON(t, w, core.MessagesResponse{
				Messages: []core.Message{{
					ID:        "assistant_1",
					SessionID: "session_1",
					RunID:     "run_1",
					Role:      core.MessageRoleAssistant,
					Content:   "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/client-deliveries/delivery_session_1/ack":
			writeJSON(t, w, core.OKResponse{OK: true})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
			PollRetryDelay:   time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.messages) != 1 {
		t.Fatalf("messages = %#v, want one assistant message", fakeAPI.messages)
	}
	if !strings.Contains(fakeAPI.messages[0].Text, "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.") {
		t.Fatalf("message text = %q, want full answer in one message", fakeAPI.messages[0].Text)
	}
}

func TestDeliverPendingRunSendsDraftWithoutThreadAndLeavesDeliveryPending(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var acked bool
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_session_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "123",
				SessionID:   "session_1",
				RunID:       "run_1",
				Address:     json.RawMessage(`{"chat_id":123,"thread_id":7}`),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_1":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_1",
				SessionID: "session_1",
				Status:    core.RunStatusRunning,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			writeJSON(t, w, core.MessagesResponse{
				Messages: []core.Message{{
					ID:        "assistant_1",
					SessionID: "session_1",
					RunID:     "run_1",
					Role:      core.MessageRoleAssistant,
					Content:   "Partial answer while the run is still active.",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/client-deliveries/delivery_session_1/ack":
			acked = true
			writeJSON(t, w, core.OKResponse{OK: true})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
			PollRetryDelay:   time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}
	if acked {
		t.Fatalf("delivery was acknowledged while run is still active")
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.drafts) != 1 {
		t.Fatalf("drafts = %#v, want one draft preview while run is active", fakeAPI.drafts)
	}
	if fakeAPI.drafts[0].ChatID != 123 {
		t.Fatalf("draft chat id = %d, want 123", fakeAPI.drafts[0].ChatID)
	}
	if fakeAPI.drafts[0].DraftID == 0 {
		t.Fatalf("draft id = 0, want stable non-zero id")
	}
	if !strings.Contains(fakeAPI.drafts[0].Text, "Partial answer while the run is still active.") {
		t.Fatalf("draft text = %q, want assistant partial text", fakeAPI.drafts[0].Text)
	}
	assertNoMessageThreadID(t, fakeAPI.drafts[0])
	if len(fakeAPI.messages) != 0 {
		t.Fatalf("messages = %#v, want no persistent chat messages while run is active", fakeAPI.messages)
	}
}

func TestDeliverPendingRunWithoutAssistantSendsBrainDraftWithoutThread(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_session_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "123",
				SessionID:   "session_1",
				RunID:       "run_1",
				Address:     json.RawMessage(`{"chat_id":123,"thread_id":7}`),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_1":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_1",
				SessionID: "session_1",
				Status:    core.RunStatusAccepted,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			writeJSON(t, w, core.MessagesResponse{Messages: nil})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
			PollRetryDelay:   time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.drafts) != 1 {
		t.Fatalf("drafts = %#v, want one thinking emoji draft", fakeAPI.drafts)
	}
	if fakeAPI.drafts[0].ChatID != 123 {
		t.Fatalf("draft chat id = %d, want 123", fakeAPI.drafts[0].ChatID)
	}
	if fakeAPI.drafts[0].DraftID == 0 {
		t.Fatalf("draft id = 0, want stable non-zero id")
	}
	if fakeAPI.drafts[0].Text != defaultThinkingDraftText {
		t.Fatalf("draft text = %q, want thinking emoji", fakeAPI.drafts[0].Text)
	}
	assertNoMessageThreadID(t, fakeAPI.drafts[0])
	if len(fakeAPI.actions) != 0 {
		t.Fatalf("actions = %#v, want no active-run chat action from draft renderer", fakeAPI.actions)
	}
}

func TestDeliverPendingGuestSessionRunAnswersGuestQueryAndAcknowledges(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	acked := make(chan struct{})
	var ackOnce sync.Once
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_guest_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "guest:guest_query_1",
				SessionID:   "session_guest",
				RunID:       "run_guest",
				Address:     encodeDeliveryAddress(DeliveryAddress{Kind: "guest", GuestQueryID: "guest_query_1"}),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_guest":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_guest",
				SessionID: "session_guest",
				Status:    core.RunStatusCompleted,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			writeJSON(t, w, core.MessagesResponse{
				Messages: []core.Message{{
					ID:        "assistant_guest_1",
					SessionID: "session_guest",
					RunID:     "run_guest",
					Role:      core.MessageRoleAssistant,
					Content:   "Guest answer.",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/client-deliveries/delivery_guest_1/ack":
			ackOnce.Do(func() { close(acked) })
			writeJSON(t, w, core.OKResponse{OK: true})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
			PollRetryDelay:   time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}

	select {
	case <-acked:
	case <-ctx.Done():
		t.Fatalf("delivery was not acknowledged: %v", ctx.Err())
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.guestAnswers) != 1 {
		t.Fatalf("guest answers = %#v, want one answer", fakeAPI.guestAnswers)
	}
	answer := fakeAPI.guestAnswers[0]
	if answer.GuestQueryID != "guest_query_1" {
		t.Fatalf("guest query id = %q", answer.GuestQueryID)
	}
	if answer.Result.InputMessageContent.MessageText == "" || !strings.Contains(answer.Result.InputMessageContent.MessageText, "Guest answer.") {
		t.Fatalf("guest message text = %q, want assistant answer", answer.Result.InputMessageContent.MessageText)
	}
	if len(fakeAPI.messages) != 0 {
		t.Fatalf("messages = %#v, want no chat messages for guest answer", fakeAPI.messages)
	}
}

func TestDeliverPendingInlineRunEditsInlineMessageAndAcknowledges(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	acked := make(chan struct{})
	var ackOnce sync.Once
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_inline_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "42",
				SessionID:   "session_inline",
				RunID:       "run_inline",
				Address:     encodeDeliveryAddress(DeliveryAddress{Kind: "inline", InlineMessageID: "inline_msg_1"}),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_inline":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_inline",
				SessionID: "session_inline",
				Status:    core.RunStatusCompleted,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			writeJSON(t, w, core.MessagesResponse{
				Messages: []core.Message{{
					ID:        "assistant_inline_1",
					SessionID: "session_inline",
					RunID:     "run_inline",
					Role:      core.MessageRoleAssistant,
					Content:   "Acme LLC bank details\n\nTax ID: 1234567890",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/client-deliveries/delivery_inline_1/ack":
			ackOnce.Do(func() { close(acked) })
			writeJSON(t, w, core.OKResponse{OK: true})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
			PollRetryDelay:   time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}

	select {
	case <-acked:
	case <-ctx.Done():
		t.Fatalf("delivery was not acknowledged: %v", ctx.Err())
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.edits) != 1 {
		t.Fatalf("edits = %#v, want one inline edit", fakeAPI.edits)
	}
	edit := fakeAPI.edits[0]
	if edit.InlineMessageID != "inline_msg_1" {
		t.Fatalf("inline message id = %q", edit.InlineMessageID)
	}
	if !strings.Contains(edit.Text, "Acme LLC bank details") {
		t.Fatalf("edit text = %q, want assistant answer", edit.Text)
	}
	if len(fakeAPI.messages) != 0 || len(fakeAPI.drafts) != 0 || len(fakeAPI.guestAnswers) != 0 {
		t.Fatalf("messages=%#v drafts=%#v guest=%#v, want only inline edit", fakeAPI.messages, fakeAPI.drafts, fakeAPI.guestAnswers)
	}
}

func TestDeliverPendingInlineRunShowsReadableThinkingTextWhileRunning(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{{
				ID:          "delivery_inline_1",
				Type:        core.ClientDeliveryTypeRun,
				Client:      "telegram",
				ExternalKey: "42",
				SessionID:   "session_inline",
				RunID:       "run_inline",
				Address:     encodeDeliveryAddress(DeliveryAddress{Kind: "inline", InlineMessageID: "inline_msg_1"}),
				Status:      core.ClientDeliveryStatusPending,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_inline":
			writeJSON(t, w, core.RunResponse{Run: core.Run{
				ID:        "run_inline",
				SessionID: "session_inline",
				Status:    core.RunStatusRunning,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			writeJSON(t, w, core.MessagesResponse{Messages: nil})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
			PollRetryDelay:   time.Millisecond,
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.deliverPendingRuns(ctx); err != nil {
		t.Fatalf("deliverPendingRuns: %v", err)
	}

	fakeAPI.mu.Lock()
	defer fakeAPI.mu.Unlock()
	if len(fakeAPI.edits) != 1 {
		t.Fatalf("edits = %#v, want one inline edit", fakeAPI.edits)
	}
	if !strings.Contains(fakeAPI.edits[0].Text, "Thinking") {
		t.Fatalf("edit text = %q, want readable thinking text", fakeAPI.edits[0].Text)
	}
	if strings.Contains(fakeAPI.edits[0].Text, "Run status") {
		t.Fatalf("edit text = %q, want no technical run status", fakeAPI.edits[0].Text)
	}
}

func TestDeliverDocumentInvalidPayloadMarksDeliveryFailed(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	var requestPath string
	var failRequest core.ClientDeliveryFailRequest
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/fail") {
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&failRequest); err != nil {
			t.Fatalf("decode fail request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(daemon.Close)
	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
		},
	}

	err := worker.deliverDocument(context.Background(), core.ClientDelivery{
		ID:          "delivery_1",
		Type:        core.ClientDeliveryTypeDocument,
		Client:      "telegram",
		ExternalKey: "123",
		Address:     encodeDeliveryAddress(DeliveryAddress{ChatID: 123}),
		Payload:     json.RawMessage(`{"storage_path":`),
	})
	if err != nil {
		t.Fatalf("deliverDocument: %v", err)
	}
	if requestPath != "/v1/client-deliveries/delivery_1/fail" {
		t.Fatalf("daemon request path = %q", requestPath)
	}
	if !strings.Contains(failRequest.Error, "invalid payload") {
		t.Fatalf("fail error = %q, want invalid payload", failRequest.Error)
	}
	if len(fakeAPI.messages) != 1 || !strings.Contains(fakeAPI.messages[0].Text, "File delivery failed") {
		t.Fatalf("messages = %#v, want one failure message", fakeAPI.messages)
	}
	if len(fakeAPI.documents) != 0 {
		t.Fatalf("documents = %#v, want none", fakeAPI.documents)
	}
}

func TestDeliverDocumentInlineTargetFailsWithoutSendingToChatZero(t *testing.T) {
	fakeAPI := &deliveryFakeBotAPI{}
	var requestPath string
	var failRequest core.ClientDeliveryFailRequest
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/fail") {
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&failRequest); err != nil {
			t.Fatalf("decode fail request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(daemon.Close)
	worker := &Worker{
		api: fakeAPI,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			DaemonHTTPClient: daemon.Client(),
		},
	}

	err := worker.deliverDocument(context.Background(), core.ClientDelivery{
		ID:          "delivery_inline",
		Type:        core.ClientDeliveryTypeDocument,
		Client:      "telegram",
		ExternalKey: "123",
		Address:     encodeDeliveryAddress(DeliveryAddress{Kind: telegramTargetInline, InlineMessageID: "inline_msg_1"}),
		Payload:     json.RawMessage(`{"storage_path":"reports/result.txt"}`),
	})
	if err != nil {
		t.Fatalf("deliverDocument: %v", err)
	}
	if requestPath != "/v1/client-deliveries/delivery_inline/fail" {
		t.Fatalf("daemon request path = %q", requestPath)
	}
	if !strings.Contains(failRequest.Error, "private chat") {
		t.Fatalf("fail error = %q, want private chat explanation", failRequest.Error)
	}
	if len(fakeAPI.documents) != 0 || len(fakeAPI.actions) != 0 {
		t.Fatalf("documents=%#v actions=%#v, want no chat_id=0 Telegram calls", fakeAPI.documents, fakeAPI.actions)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

type deliveryFakeBotAPI struct {
	mu              sync.Mutex
	messages        []SendMessageRequest
	drafts          []SendMessageDraftRequest
	actions         []SendChatActionRequest
	voices          []SendVoiceRequest
	audios          []SendAudioRequest
	documents       []SendDocumentRequest
	edits           []EditMessageTextRequest
	mediaEdits      []EditMessageMediaRequest
	deletes         []DeleteMessageRequest
	commands        []SetMyCommandsRequest
	guestAnswers    []AnswerGuestQueryRequest
	inlineAnswers   []AnswerInlineQueryRequest
	callbackAnswers []AnswerCallbackQueryRequest
	events          []string
}

func (a *deliveryFakeBotAPI) GetMe(context.Context) (User, error) { return User{}, nil }

func (a *deliveryFakeBotAPI) GetUpdates(context.Context, GetUpdatesRequest) ([]Update, error) {
	return nil, nil
}

func (a *deliveryFakeBotAPI) GetFile(context.Context, string) (File, error) { return File{}, nil }

func (a *deliveryFakeBotAPI) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }

func (a *deliveryFakeBotAPI) SendMessage(_ context.Context, req SendMessageRequest) (SentMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, req)
	return SentMessage{MessageID: int64(len(a.messages))}, nil
}

func (a *deliveryFakeBotAPI) SendMessageDraft(_ context.Context, req SendMessageDraftRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drafts = append(a.drafts, req)
	a.events = append(a.events, "draft")
	return nil
}

func (a *deliveryFakeBotAPI) SendChatAction(_ context.Context, req SendChatActionRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, req)
	a.events = append(a.events, "action")
	return nil
}

func (a *deliveryFakeBotAPI) SendVoice(_ context.Context, req SendVoiceRequest) (SentMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.voices = append(a.voices, req)
	return SentMessage{MessageID: int64(len(a.voices))}, nil
}

func (a *deliveryFakeBotAPI) SendAudio(_ context.Context, req SendAudioRequest) (SentMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.audios = append(a.audios, req)
	return SentMessage{MessageID: int64(len(a.audios)), Audio: &Audio{FileID: "audio_file_1"}}, nil
}

func (a *deliveryFakeBotAPI) SendDocument(_ context.Context, req SendDocumentRequest) (SentMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.documents = append(a.documents, req)
	return SentMessage{MessageID: int64(len(a.documents))}, nil
}

func (a *deliveryFakeBotAPI) EditMessageText(_ context.Context, req EditMessageTextRequest) (EditMessageTextResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.edits = append(a.edits, req)
	return EditMessageTextResponse{}, nil
}

func (a *deliveryFakeBotAPI) EditMessageMedia(_ context.Context, req EditMessageMediaRequest) (EditMessageMediaResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mediaEdits = append(a.mediaEdits, req)
	return EditMessageMediaResponse{}, nil
}

func (a *deliveryFakeBotAPI) AnswerCallbackQuery(_ context.Context, req AnswerCallbackQueryRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.callbackAnswers = append(a.callbackAnswers, req)
	return nil
}

func (a *deliveryFakeBotAPI) AnswerGuestQuery(_ context.Context, req AnswerGuestQueryRequest) (SentGuestMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.guestAnswers = append(a.guestAnswers, req)
	return SentGuestMessage{InlineMessageID: "inline_guest_1"}, nil
}

func (a *deliveryFakeBotAPI) AnswerInlineQuery(_ context.Context, req AnswerInlineQueryRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inlineAnswers = append(a.inlineAnswers, req)
	return nil
}

func (a *deliveryFakeBotAPI) DeleteMessage(_ context.Context, req DeleteMessageRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deletes = append(a.deletes, req)
	return nil
}

func (a *deliveryFakeBotAPI) SetMyCommands(_ context.Context, req SetMyCommandsRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.commands = append(a.commands, req)
	return nil
}

func (a *deliveryFakeBotAPI) DeleteMyCommands(context.Context, DeleteMyCommandsRequest) error {
	return nil
}

func assertNoMessageThreadID(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(data), "message_thread_id") {
		t.Fatalf("request JSON = %s, want no message_thread_id", data)
	}
}
