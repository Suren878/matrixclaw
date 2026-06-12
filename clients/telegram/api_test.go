package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendDocumentUploadsMultipartFile(t *testing.T) {
	var gotPath string
	var gotChatID string
	var gotCaption string
	var gotFileName string
	var gotContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		gotChatID = r.FormValue("chat_id")
		gotCaption = r.FormValue("caption")
		file, header, err := r.FormFile("document")
		if err != nil {
			t.Fatalf("FormFile(document): %v", err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll(document): %v", err)
		}
		gotFileName = header.Filename
		gotContent = string(content)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 42,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := client.SendDocument(context.Background(), SendDocumentRequest{
		ChatID:   123,
		Document: []byte("hello config\n"),
		FileName: "vpn.conf",
		Caption:  "VPN config",
		MIMEType: "text/plain",
	})
	if err != nil {
		t.Fatalf("SendDocument() error = %v", err)
	}
	if sent.MessageID != 42 {
		t.Fatalf("MessageID = %d, want 42", sent.MessageID)
	}
	if gotPath != "/bottest-token/sendDocument" {
		t.Fatalf("path = %q, want sendDocument path", gotPath)
	}
	if gotChatID != "123" {
		t.Fatalf("chat_id = %q, want 123", gotChatID)
	}
	if gotCaption != "VPN config" {
		t.Fatalf("caption = %q, want VPN config", gotCaption)
	}
	if gotFileName != "vpn.conf" {
		t.Fatalf("file name = %q, want vpn.conf", gotFileName)
	}
	if !strings.EqualFold(gotContent, "hello config\n") {
		t.Fatalf("document content = %q, want hello config", gotContent)
	}
}

func TestGetUpdatesTimeoutErrorRemainsRetryableAfterTokenRedaction(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Token:   "test-token",
		BaseURL: "https://api.telegram.org",
		HTTPClient: httpDoerFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("Post %q: %w", req.URL.String(), context.DeadlineExceeded)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GetUpdates(context.Background(), GetUpdatesRequest{})
	if err == nil {
		t.Fatalf("GetUpdates() error = nil, want timeout")
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatalf("error = %q, want bot token redacted", err.Error())
	}
	if !IsRetryable(err) {
		t.Fatalf("IsRetryable(%q) = false, want true", err.Error())
	}
}

func TestAnswerGuestQueryUsesBotAPI10Method(t *testing.T) {
	var gotPath string
	var gotRequest AnswerGuestQueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"inline_message_id": "inline_guest_1",
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := client.AnswerGuestQuery(context.Background(), AnswerGuestQueryRequest{
		GuestQueryID: "guest_query_1",
		Result: InlineQueryResultArticle{
			Type:  "article",
			ID:    "matrixclaw",
			Title: "Matrixclaw",
			InputMessageContent: InputTextMessageContent{
				MessageText: "hello guest",
				ParseMode:   "MarkdownV2",
			},
		},
	})
	if err != nil {
		t.Fatalf("AnswerGuestQuery() error = %v", err)
	}
	if sent.InlineMessageID != "inline_guest_1" {
		t.Fatalf("InlineMessageID = %q, want inline_guest_1", sent.InlineMessageID)
	}
	if gotPath != "/bottest-token/answerGuestQuery" {
		t.Fatalf("path = %q, want answerGuestQuery path", gotPath)
	}
	if gotRequest.GuestQueryID != "guest_query_1" {
		t.Fatalf("guest_query_id = %q", gotRequest.GuestQueryID)
	}
	if gotRequest.Result.InputMessageContent.MessageText != "hello guest" {
		t.Fatalf("message_text = %q", gotRequest.Result.InputMessageContent.MessageText)
	}
}

func TestGetMeDecodesBotAPI10CapabilityFlags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/getMe" {
			t.Fatalf("path = %q, want getMe path", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":                            123,
				"is_bot":                        true,
				"username":                      "matrixclaw_bot",
				"can_join_groups":               true,
				"can_read_all_group_messages":   true,
				"supports_guest_queries":        true,
				"supports_inline_queries":       true,
				"can_connect_to_business":       true,
				"has_topics_enabled":            true,
				"allows_users_to_create_topics": true,
				"can_manage_bots":               true,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if !user.CanJoinGroups || !user.CanReadAllGroupMessages || !user.SupportsGuestQueries || !user.SupportsInlineQueries ||
		!user.CanConnectToBusiness || !user.HasTopicsEnabled || !user.AllowsUsersToCreateTopics || !user.CanManageBots {
		t.Fatalf("user flags = %#v, want all Bot API 10 capability flags decoded", user)
	}
}

func TestAnswerInlineQueryUsesBotAPIMethod(t *testing.T) {
	var gotPath string
	var gotRequest AnswerInlineQueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": true,
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.AnswerInlineQuery(context.Background(), AnswerInlineQueryRequest{
		InlineQueryID: "inline_query_1",
		Results: []InlineQueryResultArticle{{
			Type:        "article",
			ID:          "matrixclaw",
			Title:       "Ask Matrixclaw",
			Description: "tasks in Friday",
			InputMessageContent: InputTextMessageContent{
				MessageText: "Matrixclaw is preparing an answer.",
				ParseMode:   "HTML",
			},
			ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
				{Text: "Matrixclaw", CallbackData: "inline:noop"},
			}}},
		}},
		CacheTime:  1,
		IsPersonal: true,
	})
	if err != nil {
		t.Fatalf("AnswerInlineQuery() error = %v", err)
	}
	if gotPath != "/bottest-token/answerInlineQuery" {
		t.Fatalf("path = %q, want answerInlineQuery path", gotPath)
	}
	if gotRequest.InlineQueryID != "inline_query_1" {
		t.Fatalf("inline_query_id = %q", gotRequest.InlineQueryID)
	}
	if len(gotRequest.Results) != 1 {
		t.Fatalf("results = %#v, want one result", gotRequest.Results)
	}
	result := gotRequest.Results[0]
	if result.Title != "Ask Matrixclaw" || result.Description != "tasks in Friday" {
		t.Fatalf("result = %#v, want title and description", result)
	}
	if result.ReplyMarkup == nil {
		t.Fatalf("reply_markup is nil, want inline keyboard so chosen_inline_result includes inline_message_id")
	}
	if gotRequest.CacheTime != 1 || !gotRequest.IsPersonal {
		t.Fatalf("inline request = %#v, want personal one-second cache", gotRequest)
	}
}

func TestSendMessageDraftUsesBotAPI10DraftMethod(t *testing.T) {
	var gotPath string
	var gotRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": true,
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessageDraft(context.Background(), SendMessageDraftRequest{
		ChatID:    123,
		DraftID:   99,
		Text:      "partial answer",
		ParseMode: "HTML",
	})
	if err != nil {
		t.Fatalf("SendMessageDraft() error = %v", err)
	}
	if gotPath != "/bottest-token/sendMessageDraft" {
		t.Fatalf("path = %q, want sendMessageDraft path", gotPath)
	}
	if gotRequest["chat_id"] != float64(123) || gotRequest["draft_id"] != float64(99) {
		t.Fatalf("draft request = %#v, want chat 123 draft 99", gotRequest)
	}
	if _, ok := gotRequest["message_thread_id"]; ok {
		t.Fatalf("draft request = %#v, want no message_thread_id", gotRequest)
	}
	if gotRequest["text"] != "partial answer" || gotRequest["parse_mode"] != "HTML" {
		t.Fatalf("draft request = %#v, want text and HTML parse mode", gotRequest)
	}
}

func TestEditMessageTextAcceptsInlineBooleanResult(t *testing.T) {
	var gotPath string
	var gotRequest EditMessageTextRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": true,
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.EditMessageText(context.Background(), EditMessageTextRequest{
		InlineMessageID: "inline_msg_1",
		Text:            "final inline answer",
	}); err != nil {
		t.Fatalf("EditMessageText() error = %v", err)
	}
	if gotPath != "/bottest-token/editMessageText" {
		t.Fatalf("path = %q, want editMessageText path", gotPath)
	}
	if gotRequest.InlineMessageID != "inline_msg_1" || gotRequest.Text != "final inline answer" {
		t.Fatalf("edit request = %#v, want inline edit request", gotRequest)
	}
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}
