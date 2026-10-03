package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestInlineRequestCacheKeepsOnlyRecentRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inline.json")
	worker := newWorker(Config{InlineCachePath: path}, nil)
	var first, last string
	for i := range recentInlineLimit + 10 {
		last = strings.TrimPrefix(worker.rememberInlineRequest(fmt.Sprintf("query-%d", i), fmt.Sprintf("text %d", i)), inlineCallbackPrefix)
		if i == 0 {
			first = last
		}
	}
	cached := readInlineRequestCache(path)
	if len(cached) != recentInlineLimit {
		t.Fatalf("cache holds %d requests, want %d", len(cached), recentInlineLimit)
	}
	if worker.inlineRequestText(first) != "" {
		t.Fatal("oldest request still remembered")
	}
	if got := worker.inlineRequestText(last); got != fmt.Sprintf("text %d", recentInlineLimit+9) {
		t.Fatalf("newest request = %q", got)
	}
}

func TestOnlyGuestAndInlineTargetsReplyOnce(t *testing.T) {
	for _, tc := range []struct {
		target chatTarget
		once   bool
	}{
		{chatTarget{kind: telegramTargetChat, chatID: 1}, false},
		{chatTarget{kind: telegramTargetGuest, guestQueryID: "q"}, true},
		{chatTarget{kind: telegramTargetInline, inlineMessageID: "i"}, true},
	} {
		if got := tc.target.repliesOnce(); got != tc.once {
			t.Fatalf("%s target replies once = %v, want %v", tc.target.kind, got, tc.once)
		}
	}
}

func TestAnInlineRequestWaitingForApprovalAsksInThePrivateChat(t *testing.T) {
	inline := core.ClientDelivery{ID: "delivery-1", Type: core.ClientDeliveryTypeRun, ExternalKey: "42", SessionID: "s1", RunID: "run-1", ReplyOnce: true, Address: encodeDeliveryAddress(DeliveryAddress{Kind: "inline", InlineMessageID: "im"})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/client-deliveries":
			_ = json.NewEncoder(w).Encode(core.ClientDeliveriesResponse{Deliveries: []core.ClientDelivery{inline}})
		case "/v1/runs/run-1":
			_ = json.NewEncoder(w).Encode(core.RunResponse{Run: core.Run{ID: "run-1", SessionID: "s1", Status: core.RunStatusWaitingApproval}})
		case "/v1/approvals":
			_ = json.NewEncoder(w).Encode(core.ApprovalsResponse{Approvals: []core.Approval{{ID: "a1", SessionID: "s1", RunID: "run-1", State: core.ApprovalStatePending, ToolName: "bash"}}})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := newWorker(Config{BaseURL: server.URL, AllowedUserID: 42}, api)

	for range 2 {
		if err := worker.deliverPending(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if len(api.sent) != 1 || api.sent[0].ChatID != 42 || approvalButtons(t, api.sent[0])[0] != cbApprovalOnce+"a1" {
		t.Fatalf("private chat messages = %+v", api.sent)
	}
	if last := api.edits[len(api.edits)-1]; last.InlineMessageID != "im" || !strings.Contains(last.Text, "Open the private Matrixclaw chat") {
		t.Fatalf("inline edits = %+v", api.edits)
	}
}

func TestAnInlineRequestToABusySessionSaysItWaits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/messages":
			_ = json.NewEncoder(w).Encode(core.AcceptRunResult{SessionID: "s1", Status: core.AcceptRunStatusQueued, Input: &core.SessionInput{ID: "input-1", TargetRunID: "run-1"}})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := newWorker(Config{BaseURL: server.URL, AllowedUserID: 42}, api)
	target := chatTarget{kind: telegramTargetInline, chatID: 42, inlineMessageID: "im", externalKey: "42"}

	started, err := worker.startInlineUserMessage(context.Background(), target, "weather in Riga")

	if err != nil || !started {
		t.Fatalf("started = %v, %v", started, err)
	}
	if len(api.edits) != 1 || api.edits[0].InlineMessageID != "im" || !strings.Contains(api.edits[0].Text, "busy") {
		t.Fatalf("inline edits = %+v", api.edits)
	}
}
