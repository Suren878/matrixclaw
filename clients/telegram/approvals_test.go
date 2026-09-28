package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
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
