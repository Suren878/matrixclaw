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

func TestRestartCallbackOpensConfirm(t *testing.T) {
	var restartCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/admin/restart":
			restartCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "cb-restart",
		From: &User{ID: 42},
		Message: &Message{
			MessageID: 77,
			Chat:      Chat{ID: 42, Type: "private"},
		},
		Data: commandCallbackData("/restart"),
	})
	if err != nil {
		t.Fatalf("handleCallbackQuery() error = %v", err)
	}
	if restartCalled {
		t.Fatal("restart should wait for confirmation")
	}
	if len(api.editMessageRequests) != 1 {
		t.Fatalf("editMessageRequests len = %d, want 1", len(api.editMessageRequests))
	}
	if got := api.editMessageRequests[0].Text; got != "Restart Architect?" {
		t.Fatalf("confirm text = %q", got)
	}
	markup := api.editMessageRequests[0].ReplyMarkup
	if markup == nil || len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 2 {
		t.Fatalf("confirm keyboard = %#v", markup)
	}
	if got := markup.InlineKeyboard[0][0].CallbackData; got != commandCallbackData("/restart confirm") {
		t.Fatalf("confirm callback = %q", got)
	}
}

func TestCommandCallbackDataRoundTripsColonPaths(t *testing.T) {
	data := commandCallbackData("/storage temp docs/a:b.txt promote")
	kind, command, ok := parsePickerCallbackData(data)
	if !ok {
		t.Fatalf("parsePickerCallbackData(%q) failed", data)
	}
	if kind != callbackKindCommand || command != "/storage temp docs/a:b.txt promote" {
		t.Fatalf("parsed callback = (%q, %q), want command with colon path", kind, command)
	}
}

func TestNestedPickerCancelReturnsToParentCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"binding": core.ClientBinding{Client: "telegram", ExternalKey: "42", SessionID: "session_1"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []core.Session{{ID: "session_1", Title: "docs", ProviderID: "local-ai"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/setup/providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []map[string]any{
					{"id": "local-ai", "catalog_id": "local-ai", "name": "Local AI", "configured": true, "implemented": true, "model": "gemma-4"},
				},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "cb-provider-cancel",
		From: &User{ID: 42},
		Message: &Message{
			MessageID: 77,
			Text:      "Local AI",
			Chat:      Chat{ID: 42, Type: "private"},
		},
		Data: commandCallbackData("/provider"),
	})
	if err != nil {
		t.Fatalf("handleCallbackQuery() error = %v", err)
	}
	if len(api.editMessageRequests) != 1 {
		t.Fatalf("editMessageRequests len = %d, want 1", len(api.editMessageRequests))
	}
	if got := api.editMessageRequests[0].Text; got != "Provider" {
		t.Fatalf("edited text = %q, want Provider", got)
	}
	if api.editMessageRequests[0].ReplyMarkup == nil || len(api.editMessageRequests[0].ReplyMarkup.InlineKeyboard) == 0 {
		t.Fatal("expected parent provider keyboard")
	}
}

func TestTopLevelPickerCancelDeletesMenuMessage(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "cb-provider-cancel",
		From: &User{ID: 42},
		Message: &Message{
			MessageID: 88,
			Text:      "Choose:",
			Chat:      Chat{ID: 42, Type: "private"},
		},
		Data: commandCallbackData(""),
	})
	if err != nil {
		t.Fatalf("handleCallbackQuery() error = %v", err)
	}
	if len(api.deleteMessageRequests) != 1 {
		t.Fatalf("deleteMessageRequests len = %d, want 1", len(api.deleteMessageRequests))
	}
	if got := api.deleteMessageRequests[0].MessageID; got != 88 {
		t.Fatalf("deleted message id = %d, want 88", got)
	}
	if len(api.editMessageRequests) != 0 {
		t.Fatalf("editMessageRequests len = %d, want 0", len(api.editMessageRequests))
	}
}

func TestSensitivePromptDeletesTelegramMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"binding": core.ClientBinding{Client: "telegram", ExternalKey: "42", SessionID: "session_1"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []core.Session{{ID: "session_1", Title: "docs", ProviderID: "openai"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/setup/providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []map[string]any{{"id": "anthropic", "name": "Anthropic", "configured": false, "implemented": true}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/setup/providers/anthropic":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"provider": map[string]any{"id": "anthropic", "name": "Anthropic", "configured": true, "active": true, "implemented": true},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/sessions/session_1/llm":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session": core.Session{ID: "session_1", Title: "docs", ProviderID: "anthropic", ModelID: "claude"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/session_1/system-message":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": core.Message{ID: "msg_system", SessionID: "session_1", Role: core.MessageRoleSystem, Content: "ok"},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)
	worker.setPrompt("42", controlplane.PromptData{
		Title:               "API key for Anthropic",
		SubmitCommandPrefix: "/provider key anthropic ",
		Sensitive:           true,
	})

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 2,
		Message: &Message{
			MessageID: 99,
			Text:      "sk-secret",
			Chat:      Chat{ID: 42, Type: "private"},
			From:      &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if len(api.deleteMessageRequests) != 1 {
		t.Fatalf("deleteMessageRequests len = %d, want 1", len(api.deleteMessageRequests))
	}
	if got := api.deleteMessageRequests[0].MessageID; got != 99 {
		t.Fatalf("deleted message id = %d, want 99", got)
	}
}

func TestHandleCallbackQueryResolvesApproval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/approvals/apr_1/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"approval": core.Approval{
					ID:        "apr_1",
					SessionID: "session_1",
					ToolName:  "edit",
					Action:    "write",
					Path:      "/tmp/app.go",
					State:     core.ApprovalStateApproved,
				},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "cb1",
		From: &User{ID: 42},
		Message: &Message{
			MessageID: 99,
			Chat:      Chat{ID: 42, Type: "private"},
		},
		Data: cbApprovalOnce + "apr_1",
	})
	if err != nil {
		t.Fatalf("handleCallbackQuery() error = %v", err)
	}
	if len(api.editMessageRequests) != 1 {
		t.Fatalf("editMessageRequests len = %d, want 1", len(api.editMessageRequests))
	}
	if !strings.Contains(api.editMessageRequests[0].Text, "Approved") {
		t.Fatalf("edit text = %q, want approved status", api.editMessageRequests[0].Text)
	}
}

func TestApprovalKeyboardShowsSessionOnlyForEdits(t *testing.T) {
	editKeyboard := approvalKeyboard(core.Approval{ID: "apr_1", ToolName: "edit"})
	if len(editKeyboard.InlineKeyboard[0]) != 2 {
		t.Fatalf("edit approval first row len = %d, want Allow and Session", len(editKeyboard.InlineKeyboard[0]))
	}
	if !strings.HasPrefix(editKeyboard.InlineKeyboard[0][1].CallbackData, cbApprovalSession) {
		t.Fatalf("edit session callback = %q, want %q prefix", editKeyboard.InlineKeyboard[0][1].CallbackData, cbApprovalSession)
	}

	bashKeyboard := approvalKeyboard(core.Approval{ID: "apr_2", ToolName: "bash"})
	if len(bashKeyboard.InlineKeyboard[0]) != 1 {
		t.Fatalf("bash approval first row len = %d, want only Allow", len(bashKeyboard.InlineKeyboard[0]))
	}
}
