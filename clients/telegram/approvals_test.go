package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

type approvalBotAPI struct {
	recordingBotAPI
	sent  []SendMessageRequest
	edits []EditMessageTextRequest
}

func (a *approvalBotAPI) EditMessageText(_ context.Context, request EditMessageTextRequest) (EditMessageTextResponse, error) {
	a.edits = append(a.edits, request)
	return EditMessageTextResponse{}, nil
}

func (a *approvalBotAPI) SendMessage(_ context.Context, request SendMessageRequest) (SentMessage, error) {
	a.sent = append(a.sent, request)
	return SentMessage{MessageID: int64(len(a.sent))}, nil
}

func TestDenyWithReasonSendsTheNextMessageAsTheReason(t *testing.T) {
	var path string
	var resolved core.ApprovalResolveRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/v1/approvals/") {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&resolved); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.ApprovalResponse{Approval: core.Approval{ID: "approval_1", ToolName: "bash", State: core.ApprovalStateRejected, Reason: resolved.Reason}})
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := newWorker(Config{BaseURL: server.URL}, api)
	chat := Chat{ID: 42, Type: "private"}

	if err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: chat}, Data: cbApprovalReason + "approval_1"}); err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("resolved before the reason arrived: %s", path)
	}
	if len(api.edits) != 1 || api.edits[0].MessageID != 7 || api.edits[0].ReplyMarkup != nil || !strings.Contains(api.edits[0].Text, "send the reason") {
		t.Fatalf("approval message edits = %+v, want its buttons gone", api.edits)
	}
	if err := worker.handleTextMessage(context.Background(), &Message{MessageID: 8, Chat: chat, From: &User{ID: 42}, Text: "not on production"}); err != nil {
		t.Fatal(err)
	}

	if path != "/v1/approvals/approval_1/resolve" || resolved != (core.ApprovalResolveRequest{Reason: "not on production", Restricted: true}) {
		t.Fatalf("resolved %s with %+v", path, resolved)
	}
	if last := api.sent[len(api.sent)-1].Text; !strings.Contains(last, "Denied bash: not on production") {
		t.Fatalf("reply = %q", last)
	}
}

func approvalButtons(t *testing.T, request SendMessageRequest) []string {
	t.Helper()
	markup, ok := request.ReplyMarkup.(*InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("reply markup = %#v", request.ReplyMarkup)
	}
	var buttons []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			buttons = append(buttons, button.CallbackData)
		}
	}
	return buttons
}

func TestApprovalButtonsOfferGlobalRulesOnlyInTheOwnerChat(t *testing.T) {
	suggested := core.Approval{ID: "a1", RunID: "run", State: core.ApprovalStatePending, ToolName: "bash", Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}
	plain := core.Approval{ID: "a2", RunID: "run", State: core.ApprovalStatePending, ToolName: "memory"}
	owner := chatTarget{kind: telegramTargetChat, chatID: 42, externalKey: "42"}
	guest := chatTarget{kind: telegramTargetGuest, chatID: 42, guestQueryID: "q", externalKey: "guest:q"}
	for _, tc := range []struct {
		target   chatTarget
		approval core.Approval
		want     string
	}{
		{owner, suggested, "ao:a1 as:a1 ag:a1 ad:a1 ar:a1"},
		{guest, suggested, "ao:a1 ad:a1 ar:a1"},
		{owner, plain, "ao:a2 ad:a2 ar:a2"},
	} {
		api := &approvalBotAPI{}
		worker := newWorker(Config{AllowedUserID: 42}, api)

		if err := worker.renderApprovalUpdates(context.Background(), tc.target, []core.Approval{tc.approval}, "run", newRunDeliveryState()); err != nil {
			t.Fatal(err)
		}

		if got := strings.Join(approvalButtons(t, api.sent[0]), " "); got != tc.want {
			t.Errorf("%s %s: buttons = %s, want %s", tc.target.kind, tc.approval.ID, got, tc.want)
		}
	}
}

func TestAlwaysGlobalNeedsTheOwnerChat(t *testing.T) {
	for _, allowed := range []int64{0, 42} {
		var resolved []core.ApprovalResolveRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request core.ApprovalResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			resolved = append(resolved, request)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(core.ApprovalResponse{Approval: core.Approval{ID: "a1", State: core.ApprovalStateApproved, Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}})
		}))
		api := &approvalBotAPI{}
		worker := newWorker(Config{AllowedUserID: allowed, BaseURL: server.URL}, api)

		err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: Chat{ID: 42, Type: "private"}}, Data: cbApprovalGlobal + "a1"})
		server.Close()

		if err != nil {
			t.Fatal(err)
		}
		if allowed == 0 && (len(resolved) != 0 || api.sent[0].Text != "Only the owner can keep a rule for every session.") {
			t.Fatalf("open bot: resolved = %+v sent = %+v", resolved, api.sent)
		}
		if allowed == 42 && (len(resolved) != 1 || resolved[0] != (core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeGlobal})) {
			t.Fatalf("owner chat: resolved = %+v", resolved)
		}
	}
}

func TestGuestsKeepNoRules(t *testing.T) {
	var resolved int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.ApprovalResponse{Approval: core.Approval{ID: "a1", State: core.ApprovalStateApproved}})
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := newWorker(Config{AllowedUserID: 42, BaseURL: server.URL}, api)

	for _, data := range []string{cbApprovalSession + "a1", cbApprovalGlobal + "a1"} {
		message := &Message{MessageID: 7, Chat: Chat{ID: 42, Type: "private"}, GuestQueryID: "q"}
		if err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: message, Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	if resolved != 0 {
		t.Fatalf("a guest kept %d rules", resolved)
	}
}

func TestReasonPromptLetsTheMessageThroughOnceTheApprovalIsDecidedElsewhere(t *testing.T) {
	var resolves int
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(core.ClientBindingResponse{Binding: core.ClientBinding{SessionID: "session_1"}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/approvals":
			_ = json.NewEncoder(w).Encode(core.ApprovalsResponse{})
		case strings.HasPrefix(r.URL.Path, "/v1/approvals/"):
			resolves++
			http.Error(w, "approval already resolved", http.StatusBadRequest)
		default:
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	worker := newWorker(Config{BaseURL: server.URL}, &approvalBotAPI{})
	chat := Chat{ID: 42, Type: "private"}
	if err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: chat}, Data: cbApprovalReason + "approval_1"}); err != nil {
		t.Fatal(err)
	}

	_ = worker.handleTextMessage(context.Background(), &Message{MessageID: 8, Chat: chat, From: &User{ID: 42}, Text: "what changed?"})

	if _, ok := worker.prompt("42"); ok || resolves != 0 {
		t.Fatalf("prompt kept = %v, resolves = %d", ok, resolves)
	}
	if !strings.Contains(strings.Join(bodies, "\n"), "what changed?") {
		t.Fatalf("message never reached the chat: %q", bodies)
	}
}

func TestOnlyTheOwnerChatSendsUnrestricted(t *testing.T) {
	for _, tc := range []struct {
		target     chatTarget
		restricted bool
	}{
		{chatTarget{kind: telegramTargetChat, chatID: 42, externalKey: "42"}, false},
		{chatTarget{kind: telegramTargetChat, chatID: 7, externalKey: "7"}, true},
		{chatTarget{kind: telegramTargetGuest, chatID: 42, guestQueryID: "q", externalKey: "guest:q"}, true},
	} {
		var sent []core.HandleMessageInput
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input core.HandleMessageInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			sent = append(sent, input)
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": core.ErrSessionRestricted.Error()})
		}))
		api := &approvalBotAPI{}
		worker := newWorker(Config{AllowedUserID: 42, BaseURL: server.URL}, api)

		err := worker.sendUserMessage(context.Background(), tc.target, "hi")
		server.Close()

		if err != nil || len(sent) != 1 || sent[0].Restricted != tc.restricted {
			t.Fatalf("%s %s: sent = %+v err = %v", tc.target.kind, tc.target.externalKey, sent, err)
		}
		if tc.target.isChat() && (len(api.sent) == 0 || !strings.Contains(api.sent[len(api.sent)-1].Text, "Only the owner")) {
			t.Fatalf("%s: replies = %+v", tc.target.externalKey, api.sent)
		}
	}
}

func TestRunWaitingForEventsLeavesBackgroundApprovalsToTheirOwnDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/runs/run-1":
			_ = json.NewEncoder(w).Encode(core.RunResponse{Run: core.Run{ID: "run-1", SessionID: "session-1", Status: core.RunStatusWaitingEvents}})
		case "/v1/messages":
			_ = json.NewEncoder(w).Encode(core.MessagesResponse{})
		case "/v1/approvals":
			_ = json.NewEncoder(w).Encode(core.ApprovalsResponse{Approvals: []core.Approval{{ID: "a1", SessionID: "session-1", RunID: "run-1", State: core.ApprovalStatePending, ToolName: "agent", Description: `Subagent "Writer" requested approval for mutate_state`}}})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := newWorker(Config{BaseURL: server.URL}, api)
	target := chatTarget{kind: telegramTargetChat, chatID: 42, externalKey: "42"}

	if err := worker.deliverChatRunDelivery(context.Background(), target, "session-1", "run-1", "delivery-1"); err != nil {
		t.Fatal(err)
	}

	if len(api.sent) != 1 || api.sent[0].Text != "⏸ Waiting for background work" {
		t.Fatalf("sent = %+v", api.sent)
	}
}

func TestCancelingADenialReasonAsksForTheApprovalAgain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(core.ClientBindingResponse{Binding: core.ClientBinding{SessionID: "session_1"}})
		case "/v1/approvals":
			_ = json.NewEncoder(w).Encode(core.ApprovalsResponse{Approvals: []core.Approval{{ID: "approval_1", SessionID: "session_1", RunID: "run_1", State: core.ApprovalStatePending, ToolName: "bash"}}})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := newWorker(Config{BaseURL: server.URL}, api)
	chat := Chat{ID: 42, Type: "private"}
	if err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: chat, Text: "Approval required"}, Data: cbApprovalReason + "approval_1"}); err != nil {
		t.Fatal(err)
	}

	if err := worker.handleTextMessage(context.Background(), &Message{MessageID: 8, Chat: chat, From: &User{ID: 42}, Text: "/cancel"}); err != nil {
		t.Fatal(err)
	}

	if len(api.sent) != 1 || !strings.Contains(api.sent[0].Text, "Approval required") || approvalButtons(t, api.sent[0])[0] != cbApprovalOnce+"approval_1" {
		t.Fatalf("sent = %+v", api.sent)
	}
}

func TestApprovalsDecidedOutsideTheOwnerChatAreRestricted(t *testing.T) {
	for _, allowed := range []int64{7, 42} {
		var resolved core.ApprovalResolveRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&resolved)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(core.ApprovalResponse{Approval: core.Approval{ID: "a1", State: core.ApprovalStateApproved}})
		}))
		worker := newWorker(Config{AllowedUserID: allowed, BaseURL: server.URL}, &approvalBotAPI{})

		err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: allowed}, Message: &Message{MessageID: 7, Chat: Chat{ID: 42, Type: "private"}}, Data: cbApprovalSession + "a1"})
		server.Close()

		if err != nil || resolved.Restricted != (allowed != 42) {
			t.Fatalf("owner %d: resolved = %+v, %v", allowed, resolved, err)
		}
	}
}
