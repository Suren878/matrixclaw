package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestClientGetUpdatesAndSendMessage(t *testing.T) {
	var (
		seenGetMe      bool
		seenGetUpdates GetUpdatesRequest
		seenSend       SendMessageRequest
		seenAction     SendChatActionRequest
	)

	client, err := NewClient(ClientConfig{
		Token:   "test-token",
		BaseURL: "https://api.telegram.org",
		HTTPClient: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/bottest-token/getMe":
				seenGetMe = true
				return jsonResponse(`{"ok":true,"result":{"id":999,"is_bot":true,"username":"matrixclaw_bot"}}`), nil
			case "/bottest-token/getUpdates":
				if err := json.NewDecoder(req.Body).Decode(&seenGetUpdates); err != nil {
					t.Fatalf("Decode(getUpdates) error = %v", err)
				}
				return jsonResponse(`{"ok":true,"result":[{"update_id":12,"message":{"message_id":5,"text":"hello","chat":{"id":123,"type":"private"},"from":{"id":123}}}]}`), nil
			case "/bottest-token/sendMessage":
				if err := json.NewDecoder(req.Body).Decode(&seenSend); err != nil {
					t.Fatalf("Decode(sendMessage) error = %v", err)
				}
				return jsonResponse(`{"ok":true,"result":{"message_id":9,"message_thread_id":7}}`), nil
			case "/bottest-token/sendChatAction":
				if err := json.NewDecoder(req.Body).Decode(&seenAction); err != nil {
					t.Fatalf("Decode(sendChatAction) error = %v", err)
				}
				return jsonResponse(`{"ok":true,"result":true}`), nil
			default:
				t.Fatalf("unexpected path %q", req.URL.Path)
				return nil, nil
			}
		}),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	me, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if !seenGetMe || me.ID != 999 || me.Username != "matrixclaw_bot" {
		t.Fatalf("GetMe() = %+v, want bot identity", me)
	}

	updates, err := client.GetUpdates(context.Background(), GetUpdatesRequest{
		Offset:         10,
		Limit:          5,
		TimeoutSeconds: 30,
		AllowedUpdates: []string{"message"},
	})
	if err != nil {
		t.Fatalf("GetUpdates() error = %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 12 {
		t.Fatalf("GetUpdates() = %+v, want one update 12", updates)
	}
	if seenGetUpdates.Offset != 10 || seenGetUpdates.TimeoutSeconds != 30 {
		t.Fatalf("seenGetUpdates = %+v, want offset=10 timeout=30", seenGetUpdates)
	}

	sent, err := client.SendMessage(context.Background(), SendMessageRequest{
		ChatID:          123,
		MessageThreadID: 7,
		Text:            "reply",
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if seenSend.ChatID != 123 || seenSend.MessageThreadID != 7 || seenSend.Text != "reply" {
		t.Fatalf("seenSend = %+v, want chat/thread/text", seenSend)
	}
	if sent.MessageID != 9 {
		t.Fatalf("SendMessage().MessageID = %d, want 9", sent.MessageID)
	}

	if err := client.SendChatAction(context.Background(), SendChatActionRequest{
		ChatID:          123,
		MessageThreadID: 7,
		Action:          "typing",
	}); err != nil {
		t.Fatalf("SendChatAction() error = %v", err)
	}
	if seenAction.ChatID != 123 || seenAction.MessageThreadID != 7 || seenAction.Action != "typing" {
		t.Fatalf("seenAction = %+v, want chat/thread/action", seenAction)
	}
}

func TestClientReturnsRetryableAPIError(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Token:   "test-token",
		BaseURL: "https://api.telegram.org",
		HTTPClient: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(bytes.NewBufferString(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":2}}`)),
				Header:     make(http.Header),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.SendMessage(context.Background(), SendMessageRequest{
		ChatID: 123,
		Text:   "reply",
	})
	if err == nil {
		t.Fatal("SendMessage() error = nil, want API error")
	}
	if !IsRetryable(err) {
		t.Fatalf("IsRetryable(err) = false, want true; err=%v", err)
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.RetryAfter != 2*time.Second {
		t.Fatalf("RetryAfter = %s, want 2s", apiErr.RetryAfter)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}
