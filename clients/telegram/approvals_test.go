package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

type approvalBotAPI struct {
	recordingBotAPI
	sent []SendMessageRequest
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
	worker := &Worker{
		api:     api,
		config:  Config{BaseURL: server.URL, ClientName: "telegram-test", DaemonHTTPClient: server.Client()},
		prompts: map[string]controlplane.PromptData{},
	}
	chat := Chat{ID: 42, Type: "private"}

	if err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: chat}, Data: cbApprovalReason + "approval_1"}); err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("resolved before the reason arrived: %s", path)
	}
	if err := worker.handleTextMessage(context.Background(), &Message{MessageID: 8, Chat: chat, From: &User{ID: 42}, Text: "not on production"}); err != nil {
		t.Fatal(err)
	}

	if path != "/v1/approvals/approval_1/resolve" || resolved != (core.ApprovalResolveRequest{Reason: "not on production"}) {
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
		{guest, suggested, "ao:a1 as:a1 ad:a1 ar:a1"},
		{owner, plain, "ao:a2 ad:a2 ar:a2"},
	} {
		api := &approvalBotAPI{}
		worker := &Worker{api: api, config: Config{AllowedUserID: 42}}

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
		worker := &Worker{api: api, config: Config{AllowedUserID: allowed, BaseURL: server.URL, ClientName: "telegram-test", DaemonHTTPClient: server.Client()}, prompts: map[string]controlplane.PromptData{}}

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
	worker := &Worker{
		api:     &approvalBotAPI{},
		config:  Config{BaseURL: server.URL, ClientName: "telegram-test", DaemonHTTPClient: server.Client()},
		prompts: map[string]controlplane.PromptData{},
	}
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
