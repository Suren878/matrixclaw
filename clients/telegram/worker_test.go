package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestWorkerRunRetriesGetUpdatesConflict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	api := &conflictRetryBotAPI{cancel: cancel}
	worker := &Worker{
		api: api,
		config: Config{
			PollLimit:      1,
			PollTimeout:    time.Second,
			PollRetryDelay: time.Nanosecond,
		},
		offset:    &atomic.Int64{},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want retry then clean context stop", err)
	}
	if got := api.calls.Load(); got != 2 {
		t.Fatalf("GetUpdates calls = %d, want retry after conflict", got)
	}
}

func TestWorkerRunDeliversPendingRunWhileGetUpdatesIsLongPolling(t *testing.T) {
	api := &blockingUpdatesBotAPI{updatesStarted: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messagesListed := make(chan struct{})
	var messagesListedOnce sync.Once
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			if got := r.URL.Query().Get("type"); got != core.ClientDeliveryTypeRun {
				writeJSON(t, w, core.ClientDeliveriesResponse{})
				return
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
				Status:    core.RunStatusRunning,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages":
			messagesListedOnce.Do(func() { close(messagesListed) })
			writeJSON(t, w, core.MessagesResponse{
				Messages: []core.Message{{
					ID:        "assistant_1",
					SessionID: "session_1",
					RunID:     "run_1",
					Role:      core.MessageRoleAssistant,
					Content:   "Streaming while updates are long-polling.",
				}},
			})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:                 daemon.URL,
			ClientName:              "telegram",
			DaemonHTTPClient:        daemon.Client(),
			PollRetryDelay:          time.Millisecond,
			StreamFlushInterval:     20 * time.Millisecond,
			SkipCommandRegistration: true,
		},
		offset:    &atomic.Int64{},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("worker did not stop")
		}
	})

	select {
	case <-api.updatesStarted:
	case <-time.After(time.Second):
		t.Fatalf("getUpdates did not start")
	}

	select {
	case <-messagesListed:
	case <-time.After(time.Second):
		t.Fatalf("delivery loop did not poll messages while getUpdates was blocked")
	}
	waitForWorkerDraft(t, api)
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.drafts) != 1 {
		t.Fatalf("drafts = %#v, want one draft preview", api.drafts)
	}
	if !strings.Contains(api.drafts[0].Text, "Streaming while updates are long-polling.") {
		t.Fatalf("draft text = %q, want streaming assistant text", api.drafts[0].Text)
	}
	if len(api.messages) != 0 {
		t.Fatalf("messages = %#v, want no chat messages while run is active", api.messages)
	}
}

func waitForWorkerDraft(t *testing.T, api *blockingUpdatesBotAPI) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		api.mu.Lock()
		n := len(api.drafts)
		api.mu.Unlock()
		if n > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for draft preview")
		case <-tick.C:
		}
	}
}

func TestPollOnceRequestsBotAPI10Updates(t *testing.T) {
	api := &guestUpdatesBotAPI{}
	worker := &Worker{
		api: api,
		config: Config{
			PollLimit:   7,
			PollTimeout: time.Second,
		},
		offset: &atomic.Int64{},
	}

	if err := worker.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if !containsString(api.req.AllowedUpdates, "guest_message") {
		t.Fatalf("allowed updates = %#v, want guest_message", api.req.AllowedUpdates)
	}
	if !containsString(api.req.AllowedUpdates, "inline_query") {
		t.Fatalf("allowed updates = %#v, want inline_query", api.req.AllowedUpdates)
	}
	if !containsString(api.req.AllowedUpdates, "chosen_inline_result") {
		t.Fatalf("allowed updates = %#v, want chosen_inline_result", api.req.AllowedUpdates)
	}
}

func TestGuestMessageCreatesRunWithGuestDeliveryAddress(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var request core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_guest",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_guest",
					SessionID: "session_guest",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		GuestMessage: &Message{
			MessageID:    9,
			GuestQueryID: "guest_query_1",
			From:         &User{ID: 42},
			Chat:         Chat{ID: -100123, Type: "supergroup"},
			Text:         "hello from guest mode",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate guest: %v", err)
	}
	if request.Client != "telegram" {
		t.Fatalf("client = %q, want telegram", request.Client)
	}
	if request.ExternalKey != "guest:guest_query_1" {
		t.Fatalf("external key = %q, want guest:guest_query_1", request.ExternalKey)
	}
	if request.Text != "hello from guest mode" {
		t.Fatalf("text = %q", request.Text)
	}
	var address DeliveryAddress
	if err := json.Unmarshal(request.DeliveryAddress, &address); err != nil {
		t.Fatalf("decode delivery address: %v", err)
	}
	if address.Kind != "guest" || address.GuestQueryID != "guest_query_1" {
		t.Fatalf("delivery address = %#v, want guest query address", address)
	}
	if len(api.actions) != 0 {
		t.Fatalf("chat actions = %#v, want none for guest delivery", api.actions)
	}
	if len(api.messages) != 0 {
		t.Fatalf("messages = %#v, want no direct chat messages for guest delivery", api.messages)
	}
}

func TestInlineQueryAnswersWithPlaceholderArticle(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	worker := &Worker{
		api: api,
		config: Config{
			ClientName:    "telegram",
			AllowedUserID: 42,
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		InlineQuery: &InlineQuery{
			ID:    "inline_query_1",
			From:  &User{ID: 42},
			Query: "tasks on Friday",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate inline query: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.inlineAnswers) != 1 {
		t.Fatalf("inline answers = %#v, want one answer", api.inlineAnswers)
	}
	answer := api.inlineAnswers[0]
	if answer.InlineQueryID != "inline_query_1" {
		t.Fatalf("inline query id = %q", answer.InlineQueryID)
	}
	if len(answer.Results) != 1 {
		t.Fatalf("results = %#v, want one placeholder result", answer.Results)
	}
	result := answer.Results[0]
	if !strings.Contains(result.Title, "Tap") {
		t.Fatalf("title = %q, want explicit tap instruction", result.Title)
	}
	if !strings.Contains(result.Description, "tasks on Friday") {
		t.Fatalf("description = %q, want original query", result.Description)
	}
	if !strings.Contains(result.InputMessageContent.MessageText, "Thinking") {
		t.Fatalf("message text = %q, want readable thinking placeholder", result.InputMessageContent.MessageText)
	}
	if result.ReplyMarkup == nil {
		t.Fatalf("reply markup is nil, want keyboard for inline_message_id feedback")
	}
	if !strings.HasPrefix(result.ReplyMarkup.InlineKeyboard[0][0].CallbackData, inlineCallbackPrefix) {
		t.Fatalf("callback data = %q, want fallback callback that starts the inline run", result.ReplyMarkup.InlineKeyboard[0][0].CallbackData)
	}
	if !answer.IsPersonal || answer.CacheTime > 5 {
		t.Fatalf("inline answer = %#v, want personal short-lived cache", answer)
	}
}

func TestInlineResultButtonCreatesRunWhenChosenInlineFeedbackIsMissing(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var messageRequest core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_inline",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_inline",
					SessionID: "session_inline",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		InlineQuery: &InlineQuery{
			ID:    "inline_query_1",
			From:  &User{ID: 42},
			Query: "send Acme LLC bank details",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate inline query: %v", err)
	}
	api.mu.Lock()
	callbackData := api.inlineAnswers[0].Results[0].ReplyMarkup.InlineKeyboard[0][0].CallbackData
	api.mu.Unlock()

	err = worker.handleUpdate(context.Background(), Update{
		CallbackQuery: &CallbackQuery{
			ID:              "callback_inline_1",
			From:            &User{ID: 42},
			InlineMessageID: "inline_msg_1",
			Data:            callbackData,
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate inline callback: %v", err)
	}
	if messageRequest.Client != "telegram" || messageRequest.ExternalKey != "42" {
		t.Fatalf("message request = %#v, want main user binding", messageRequest)
	}
	if !strings.Contains(messageRequest.Text, "send Acme LLC bank details") {
		t.Fatalf("text = %q, want original inline query", messageRequest.Text)
	}
	var address DeliveryAddress
	if err := json.Unmarshal(messageRequest.DeliveryAddress, &address); err != nil {
		t.Fatalf("decode delivery address: %v", err)
	}
	if address.Kind != "inline" || address.InlineMessageID != "inline_msg_1" {
		t.Fatalf("delivery address = %#v, want inline message address", address)
	}
}

func TestInlineResultButtonSurvivesWorkerRestart(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	cachePath := t.TempDir() + "/inline-cache.json"
	var messageRequest core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_inline",
				Status:    core.AcceptRunStatusStarted,
				Run:       core.Run{ID: "run_inline", SessionID: "session_inline", Status: core.RunStatusAccepted},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	firstWorker := &Worker{
		api: api,
		config: Config{
			ClientName:      "telegram",
			AllowedUserID:   42,
			InlineCachePath: cachePath,
		},
	}
	if err := firstWorker.handleUpdate(context.Background(), Update{
		InlineQuery: &InlineQuery{ID: "inline_query_1", From: &User{ID: 42}, Query: "send Acme LLC bank details"},
	}); err != nil {
		t.Fatalf("handleUpdate inline query: %v", err)
	}
	api.mu.Lock()
	callbackData := api.inlineAnswers[0].Results[0].ReplyMarkup.InlineKeyboard[0][0].CallbackData
	api.mu.Unlock()

	restartedWorker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			InlineCachePath:  cachePath,
			DaemonHTTPClient: daemon.Client(),
		},
	}
	if err := restartedWorker.handleUpdate(context.Background(), Update{
		CallbackQuery: &CallbackQuery{
			ID:              "callback_inline_1",
			From:            &User{ID: 42},
			InlineMessageID: "inline_msg_1",
			Data:            callbackData,
		},
	}); err != nil {
		t.Fatalf("handleUpdate inline callback after restart: %v", err)
	}
	if !strings.Contains(messageRequest.Text, "send Acme LLC bank details") {
		t.Fatalf("text = %q, want cached inline query", messageRequest.Text)
	}
}

func TestInlineResultUsesCurrentTelegramBindingInsteadOfSessionPicker(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var requests []core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			var request core.HandleMessageInput
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			requests = append(requests, request)
			if len(requests) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(core.ErrorResponse{Error: "session selection required"})
				return
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_current",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_inline",
					SessionID: "session_current",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			if got := r.URL.Query().Get("external_key"); got != "42" {
				t.Fatalf("current binding external_key = %q, want 42", got)
			}
			writeJSON(t, w, core.ClientBindingResponse{Binding: core.ClientBinding{
				Client:      "telegram",
				ExternalKey: "42",
				SessionID:   "session_current",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.sendInlineUserMessage(context.Background(), chatTarget{
		kind:            telegramTargetInline,
		chatID:          42,
		inlineMessageID: "inline_msg_1",
		externalKey:     "42",
	}, "tasks on Friday")
	if err != nil {
		t.Fatalf("sendInlineUserMessage: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("message requests = %#v, want first conflict then retry with session", requests)
	}
	if requests[0].SessionID != "" {
		t.Fatalf("first request session_id = %q, want empty", requests[0].SessionID)
	}
	if requests[1].SessionID != "session_current" {
		t.Fatalf("retry session_id = %q, want current binding session", requests[1].SessionID)
	}
	if len(api.inlineAnswers) != 0 || len(api.messages) != 0 {
		t.Fatalf("inline answers=%#v messages=%#v, want no session picker transport", api.inlineAnswers, api.messages)
	}
}

func TestChosenInlineResultCreatesRunWithInlineDeliveryAddress(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var messageRequest core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_inline",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_inline",
					SessionID: "session_inline",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		ChosenInlineResult: &ChosenInlineResult{
			ResultID:        "matrixclaw",
			From:            &User{ID: 42},
			InlineMessageID: "inline_msg_1",
			Query:           "send Acme LLC bank details",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate chosen inline result: %v", err)
	}
	if messageRequest.Client != "telegram" || messageRequest.ExternalKey != "42" {
		t.Fatalf("message request = %#v, want main user binding", messageRequest)
	}
	if !strings.Contains(messageRequest.Text, "send Acme LLC bank details") {
		t.Fatalf("text = %q, want original inline query", messageRequest.Text)
	}
	if messageRequest.WorkingDir != "/repo" {
		t.Fatalf("working dir = %q, want /repo", messageRequest.WorkingDir)
	}
	var address DeliveryAddress
	if err := json.Unmarshal(messageRequest.DeliveryAddress, &address); err != nil {
		t.Fatalf("decode delivery address: %v", err)
	}
	if address.Kind != "inline" || address.InlineMessageID != "inline_msg_1" {
		t.Fatalf("delivery address = %#v, want inline message address", address)
	}
	if len(api.actions) != 0 || len(api.messages) != 0 {
		t.Fatalf("actions=%#v messages=%#v, want no direct chat transport", api.actions, api.messages)
	}
}

func TestInlinePlaceholderMessageDoesNotCreateChatRun(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var messageRequests int
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			messageRequests++
			t.Fatalf("unexpected daemon message request for inline placeholder")
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			DaemonHTTPClient: daemon.Client(),
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}

	err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 101,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 42, Type: "private"},
			Text:      "Thinking about:\n\nhow are you",
			ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
				{Text: "Get answer", CallbackData: inlineCallbackPrefix + "token"},
			}}},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate inline placeholder message: %v", err)
	}
	if messageRequests != 0 {
		t.Fatalf("message requests = %d, want none", messageRequests)
	}
	if len(api.messages) != 0 || len(api.drafts) != 0 || len(api.actions) != 0 {
		t.Fatalf("transport messages=%#v drafts=%#v actions=%#v, want no response", api.messages, api.drafts, api.actions)
	}
}

func TestDuplicateTelegramMessageUpdateDoesNotCreateSecondRun(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var requests []core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			var request core.HandleMessageInput
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			requests = append(requests, request)
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_main",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_main",
					SessionID: "session_main",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			DaemonHTTPClient: daemon.Client(),
		},
		states:    map[string]*runDeliveryState{},
		prompts:   map[string]controlplane.PromptData{},
		callbacks: map[string]string{},
		autoEdits: map[string]struct{}{},
	}
	update := Update{
		Message: &Message{
			MessageID: 202,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 42, Type: "private"},
			Text:      "Test",
		},
	}
	if err := worker.handleUpdate(context.Background(), update); err != nil {
		t.Fatalf("handleUpdate first message: %v", err)
	}
	if err := worker.handleUpdate(context.Background(), update); err != nil {
		t.Fatalf("handleUpdate duplicate message: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("message requests = %#v, want one request for duplicate Telegram message", requests)
	}
}

func TestInlineButtonDoesNotStartDuplicateRunAfterChosenInlineResult(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var requests []core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			var request core.HandleMessageInput
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			requests = append(requests, request)
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_inline",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_inline",
					SessionID: "session_inline",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		InlineQuery: &InlineQuery{
			ID:    "inline_query_1",
			From:  &User{ID: 42},
			Query: "send Acme LLC bank details",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate inline query: %v", err)
	}
	api.mu.Lock()
	callbackData := api.inlineAnswers[0].Results[0].ReplyMarkup.InlineKeyboard[0][0].CallbackData
	api.mu.Unlock()

	err = worker.handleUpdate(context.Background(), Update{
		ChosenInlineResult: &ChosenInlineResult{
			ResultID:        "matrixclaw",
			From:            &User{ID: 42},
			InlineMessageID: "inline_msg_1",
			Query:           "send Acme LLC bank details",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate chosen inline result: %v", err)
	}
	err = worker.handleUpdate(context.Background(), Update{
		CallbackQuery: &CallbackQuery{
			ID:              "callback_inline_1",
			From:            &User{ID: 42},
			InlineMessageID: "inline_msg_1",
			Data:            callbackData,
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate inline callback: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("message requests = %#v, want only chosen_inline_result to start a run", requests)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.callbackAnswers) != 1 || !strings.Contains(strings.ToLower(api.callbackAnswers[0].Text), "already") {
		t.Fatalf("callback answers = %#v, want already-running hint", api.callbackAnswers)
	}
}

func TestMessageWithLegacyThreadIDUsesMainChatBinding(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var messageRequest core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_main",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_main",
					SessionID: "session_main",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}

	var update Update
	if err := json.Unmarshal([]byte(`{
		"message": {
			"message_id": 11,
			"message_thread_id": 9,
			"from": {"id": 42},
			"chat": {"id": 777, "type": "private"},
			"text": "legacy threaded hello"
		}
	}`), &update); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	err := worker.handleUpdate(context.Background(), update)
	if err != nil {
		t.Fatalf("handleUpdate legacy threaded message: %v", err)
	}
	if messageRequest.ExternalKey != "777" || messageRequest.Text != "legacy threaded hello" {
		t.Fatalf("message request = %#v, want main chat message", messageRequest)
	}
	var address DeliveryAddress
	if err := json.Unmarshal(messageRequest.DeliveryAddress, &address); err != nil {
		t.Fatalf("decode delivery address: %v", err)
	}
	if address.ChatID != 777 {
		t.Fatalf("delivery address = %#v, want main chat without thread", address)
	}
	if len(api.actions) != 1 || api.actions[0].ChatID != 777 {
		t.Fatalf("chat actions = %#v, want typing in main chat", api.actions)
	}
}

func TestLocationMessageCreatesRunWithCoordinates(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var messageRequest core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_location",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_location",
					SessionID: "session_location",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 12,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Location:  &Location{Latitude: 43.238949, Longitude: 76.889709},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate location: %v", err)
	}
	if messageRequest.ExternalKey != "777" {
		t.Fatalf("message request = %#v, want main chat binding", messageRequest)
	}
	if !strings.Contains(messageRequest.Text, "43.238949") || !strings.Contains(messageRequest.Text, "76.889709") {
		t.Fatalf("text = %q, want coordinates", messageRequest.Text)
	}
	if !strings.Contains(messageRequest.Text, "maps.google.com") {
		t.Fatalf("text = %q, want map link", messageRequest.Text)
	}
}

func TestNearbyMessageAsksForLocationWhenMissing(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var messageRequests int
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
			messageRequests++
			t.Fatalf("unexpected daemon message request before Telegram location is shared")
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 13,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Text:      "find restaurants nearby",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate nearby message: %v", err)
	}
	if messageRequests != 0 {
		t.Fatalf("message requests = %d, want none before location", messageRequests)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.messages) != 1 {
		t.Fatalf("messages = %#v, want one location request message", api.messages)
	}
	if !strings.Contains(strings.ToLower(api.messages[0].Text), "location") {
		t.Fatalf("message text = %q, want location request", api.messages[0].Text)
	}
	payload, err := json.Marshal(api.messages[0])
	if err != nil {
		t.Fatalf("marshal send message request: %v", err)
	}
	if !strings.Contains(string(payload), `"request_location":true`) {
		t.Fatalf("send message JSON = %s, want request_location button", payload)
	}
}

func TestSharedLocationResumesPendingNearbyMessage(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var requests []core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			var request core.HandleMessageInput
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			requests = append(requests, request)
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_location",
				Status:    core.AcceptRunStatusStarted,
				Run: core.Run{
					ID:        "run_location",
					SessionID: "session_location",
					Status:    core.RunStatusAccepted,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	if err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 14,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Text:      "find restaurants nearby",
		},
	}); err != nil {
		t.Fatalf("handleUpdate nearby message: %v", err)
	}
	if err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 15,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Location:  &Location{Latitude: 43.238949, Longitude: 76.889709},
		},
	}); err != nil {
		t.Fatalf("handleUpdate shared location: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("message requests = %#v, want one request after location", requests)
	}
	if !strings.Contains(requests[0].Text, "find restaurants nearby") {
		t.Fatalf("text = %q, want pending nearby request", requests[0].Text)
	}
	if !strings.Contains(requests[0].Text, "43.238949") || !strings.Contains(requests[0].Text, "76.889709") {
		t.Fatalf("text = %q, want coordinates", requests[0].Text)
	}
}

func TestNearbyMessageUsesFreshSharedLocation(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	var requests []core.HandleMessageInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			var request core.HandleMessageInput
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			requests = append(requests, request)
			writeJSON(t, w, core.AcceptRunResult{
				SessionID: "session_location",
				Status:    core.AcceptRunStatusStarted,
				Run:       core.Run{ID: "run_location", SessionID: "session_location", Status: core.RunStatusAccepted},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/client-deliveries":
			writeJSON(t, w, core.ClientDeliveriesResponse{})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
		locations: map[string]telegramLocationContext{
			"777": {
				Location: Location{Latitude: 43.238949, Longitude: 76.889709},
				SharedAt: now.Add(-10 * time.Minute),
			},
		},
		now: func() time.Time { return now },
	}
	err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 16,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Text:      "find coffee nearby",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate nearby message: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("message requests = %#v, want one request with fresh location", requests)
	}
	if !strings.Contains(requests[0].Text, "find coffee nearby") {
		t.Fatalf("text = %q, want original request", requests[0].Text)
	}
	if !strings.Contains(requests[0].Text, "43.238949") || !strings.Contains(requests[0].Text, "76.889709") {
		t.Fatalf("text = %q, want fresh location coordinates", requests[0].Text)
	}
	if len(api.messages) != 0 {
		t.Fatalf("messages = %#v, want no location refresh request", api.messages)
	}
}

func TestNearbyMessageRefreshesStaleSharedLocation(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	var messageRequests int
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
			messageRequests++
			t.Fatalf("unexpected daemon message request with stale Telegram location")
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			DaemonHTTPClient: daemon.Client(),
		},
		locations: map[string]telegramLocationContext{
			"777": {
				Location: Location{Latitude: 43.238949, Longitude: 76.889709},
				SharedAt: now.Add(-31 * time.Minute),
			},
		},
		now: func() time.Time { return now },
	}
	err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 17,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Text:      "find coffee nearby",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate nearby message: %v", err)
	}
	if messageRequests != 0 {
		t.Fatalf("message requests = %d, want none before refreshed location", messageRequests)
	}
	if len(api.messages) != 1 {
		t.Fatalf("messages = %#v, want one location refresh request", api.messages)
	}
	payload, err := json.Marshal(api.messages[0])
	if err != nil {
		t.Fatalf("marshal send message request: %v", err)
	}
	if !strings.Contains(string(payload), `"request_location":true`) {
		t.Fatalf("send message JSON = %s, want request_location button", payload)
	}
}

func TestNewCommandCreatesMatrixclawSessionWithoutTelegramTopicAPI(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var createRequest core.CreateSessionRequest
	var bindingRequest core.UseBindingInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Fatalf("decode create session request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(core.SessionResponse{Session: core.Session{ID: "session_main", Title: "New chat"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bindings/use":
			if err := json.NewDecoder(r.Body).Decode(&bindingRequest); err != nil {
				t.Fatalf("decode binding request: %v", err)
			}
			writeJSON(t, w, core.ClientBindingResponse{Binding: core.ClientBinding{
				Client:      "telegram",
				ExternalKey: "777",
				SessionID:   "session_main",
			}})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			WorkingDir:       "/repo",
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		Message: &Message{
			MessageID: 21,
			From:      &User{ID: 42},
			Chat:      Chat{ID: 777, Type: "private"},
			Text:      "/new Refactor Telegram",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate new session: %v", err)
	}
	if createRequest.Title != "Refactor Telegram" || createRequest.RuntimeID != "matrixclaw" || createRequest.WorkingDir != "/repo" {
		t.Fatalf("create session request = %#v, want Matrixclaw main chat session", createRequest)
	}
	if bindingRequest.Client != "telegram" || bindingRequest.ExternalKey != "777" || bindingRequest.SessionID != "session_main" {
		t.Fatalf("binding request = %#v, want main chat binding", bindingRequest)
	}
	if len(api.messages) != 1 || api.messages[0].ChatID != 777 {
		t.Fatalf("messages = %#v, want one main chat response", api.messages)
	}
	assertNoMessageThreadID(t, api.messages[0])
}

func TestSessionsPickerCallbacksUseMainChatBinding(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	var bindingRequest core.UseBindingInput
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bindings/use":
			if err := json.NewDecoder(r.Body).Decode(&bindingRequest); err != nil {
				t.Fatalf("decode binding request: %v", err)
			}
			writeJSON(t, w, core.ClientBindingResponse{Binding: core.ClientBinding{
				Client:      "telegram",
				ExternalKey: "777",
				SessionID:   "session_main",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			if got := r.URL.Query().Get("external_key"); got != "777" {
				t.Errorf("current binding external_key = %q, want 777", got)
			}
			writeJSON(t, w, core.ClientBindingResponse{Binding: core.ClientBinding{
				Client:      "telegram",
				ExternalKey: "777",
				SessionID:   "session_main",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			writeJSON(t, w, core.SessionsResponse{Sessions: []core.Session{{
				ID:        "session_main",
				Title:     "Main chat",
				RuntimeID: core.SessionRuntimeMatrixClaw,
			}}})
		default:
			t.Errorf("unexpected daemon request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          daemon.URL,
			ClientName:       "telegram",
			AllowedUserID:    42,
			DaemonHTTPClient: daemon.Client(),
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		CallbackQuery: &CallbackQuery{
			ID:   "callback_1",
			From: &User{ID: 42},
			Message: &Message{
				MessageID: 22,
				From:      &User{ID: 42},
				Chat:      Chat{ID: 777, Type: "private"},
			},
			Data: commandCallbackData("/session use session_main"),
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate session use callback: %v", err)
	}
	if bindingRequest.Client != "telegram" || bindingRequest.ExternalKey != "777" || bindingRequest.SessionID != "session_main" {
		t.Fatalf("binding request = %#v, want main chat binding", bindingRequest)
	}
	if len(api.messages) != 0 {
		t.Fatalf("messages = %#v, want successful edit without fallback send", api.messages)
	}
}

func TestExpiredCompactCallbackAnswersWithMenuExpiredHint(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	worker := &Worker{
		api: api,
		config: Config{
			ClientName:    "telegram",
			AllowedUserID: 42,
		},
	}

	err := worker.handleUpdate(context.Background(), Update{
		CallbackQuery: &CallbackQuery{
			ID:   "callback_stale",
			From: &User{ID: 42},
			Message: &Message{
				MessageID: 22,
				From:      &User{ID: 42},
				Chat:      Chat{ID: 777, Type: "private"},
			},
			Data: cbCallbackRef + "missing",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate expired callback: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.callbackAnswers) != 1 {
		t.Fatalf("callback answers = %#v, want one", api.callbackAnswers)
	}
	if !strings.Contains(strings.ToLower(api.callbackAnswers[0].Text), "expired") {
		t.Fatalf("callback answer text = %q, want expired hint", api.callbackAnswers[0].Text)
	}
	if len(api.messages) != 0 || len(api.edits) != 0 {
		t.Fatalf("messages=%#v edits=%#v, want no stale menu dispatch", api.messages, api.edits)
	}
}

func TestGuestTextToSpeechCommandReturnsTextAnswer(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	worker := &Worker{
		api: api,
		config: Config{
			AllowedUserID: 42,
			ClientName:    "telegram",
		},
	}
	err := worker.handleUpdate(context.Background(), Update{
		GuestMessage: &Message{
			MessageID:    9,
			GuestQueryID: "guest_query_tts",
			From:         &User{ID: 42},
			Chat:         Chat{ID: -100123, Type: "supergroup"},
			Text:         "/tts hello",
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate guest tts: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.guestAnswers) != 1 {
		t.Fatalf("guest answers = %#v, want one text answer", api.guestAnswers)
	}
	if !strings.Contains(api.guestAnswers[0].Result.InputMessageContent.MessageText, "Guest mode supports text answers only") {
		t.Fatalf("guest answer = %q", api.guestAnswers[0].Result.InputMessageContent.MessageText)
	}
	if len(api.actions) != 0 || len(api.messages) != 0 {
		t.Fatalf("actions=%#v messages=%#v, want no chat transport", api.actions, api.messages)
	}
}

type conflictRetryBotAPI struct {
	deliveryFakeBotAPI
	calls  atomic.Int64
	cancel context.CancelFunc
}

func (a *conflictRetryBotAPI) GetUpdates(ctx context.Context, _ GetUpdatesRequest) ([]Update, error) {
	call := a.calls.Add(1)
	if call == 1 {
		return nil, &APIError{
			Method:      "getUpdates",
			StatusCode:  http.StatusConflict,
			ErrorCode:   http.StatusConflict,
			Description: "Conflict: terminated by other getUpdates request",
		}
	}
	a.cancel()
	return nil, ctx.Err()
}

type guestUpdatesBotAPI struct {
	deliveryFakeBotAPI
	req GetUpdatesRequest
}

func (a *guestUpdatesBotAPI) GetUpdates(_ context.Context, req GetUpdatesRequest) ([]Update, error) {
	a.req = req
	return nil, nil
}

type blockingUpdatesBotAPI struct {
	deliveryFakeBotAPI
	updatesStarted chan struct{}
	once           sync.Once
}

func (a *blockingUpdatesBotAPI) GetUpdates(ctx context.Context, _ GetUpdatesRequest) ([]Update, error) {
	a.once.Do(func() { close(a.updatesStarted) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
