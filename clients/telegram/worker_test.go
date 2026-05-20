package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func TestHandleUpdateNewSessionCreatesAndBindsSession(t *testing.T) {
	var (
		createCalled bool
		useCalled    bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			createCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session": core.Session{ID: "session_1", Title: "docs"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bindings/use":
			useCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"binding": core.ClientBinding{Client: "telegram", ExternalKey: "42", SessionID: "session_1"},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 1,
			Text:      "/new docs",
			Chat:      Chat{ID: 42, Type: "private"},
			From:      &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if !createCalled || !useCalled {
		t.Fatalf("createCalled=%v useCalled=%v, want both true", createCalled, useCalled)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	if !strings.Contains(api.sendMessageRequests[0].Text, "docs") {
		t.Fatalf("reply text = %q, want session title", api.sendMessageRequests[0].Text)
	}
}

func TestHandleUpdatePhotoSendsImagePart(t *testing.T) {
	var messageRequest core.HandleMessageInput
	var storageRequest localstorage.FileSaveRequest
	apiFileContent := []byte("png-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/storage/temp":
			if err := json.NewDecoder(r.Body).Decode(&storageRequest); err != nil {
				t.Fatalf("decode storage request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(localstorage.TempFileResponse{
				File: localstorage.TempEntry{
					Path:     "telegram/images/photo.png",
					Title:    "image.png",
					MIMEType: "image/png",
					Size:     int64(len(apiFileContent)),
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(core.AcceptRunResult{
				SessionID: "session_1",
				Run:       core.Run{ID: "run_1"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{
		file:        File{FileID: "photo_big", FilePath: "photos/image.png"},
		fileContent: apiFileContent,
	}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 3,
		Message: &Message{
			MessageID: 3,
			Caption:   "what is this?",
			Photo: []PhotoSize{
				{FileID: "photo_small", Width: 10, Height: 10},
				{FileID: "photo_big", Width: 100, Height: 100},
			},
			Chat: Chat{ID: 42, Type: "private"},
			From: &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if messageRequest.Text != "what is this?" {
		t.Fatalf("Text = %q, want caption", messageRequest.Text)
	}
	if len(messageRequest.Parts) != 2 || messageRequest.Parts[1].Image == nil {
		t.Fatalf("Parts = %#v, want text and image", messageRequest.Parts)
	}
	if storageRequest.Path == "" || storageRequest.Title != "image.png" || storageRequest.MIMEType != "image/png" {
		t.Fatalf("storage request = %#v, want temporary image upload", storageRequest)
	}
	image := messageRequest.Parts[1].Image
	if image.MIMEType != "image/png" || image.Name != "image.png" || image.StoragePath != "telegram/images/photo.png" || !image.Temporary || image.DataBase64 != "" {
		t.Fatalf("Image = %#v, want temporary storage reference", image)
	}
}

func TestHandleUpdateConflictRendersSharedSessionPicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "session selection required"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []core.Session{
					{ID: "session_1", Title: "docs"},
					{ID: "session_2", Title: "ops"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "binding not found"})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 2,
		Message: &Message{
			MessageID: 2,
			Text:      "hello",
			Chat:      Chat{ID: 42, Type: "private"},
			From:      &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	request := api.sendMessageRequests[0]
	if !strings.Contains(request.Text, "Choose a session or create a new one") {
		t.Fatalf("request.Text = %q, want session picker prompt", request.Text)
	}
	if request.ReplyMarkup == nil || len(request.ReplyMarkup.InlineKeyboard) != 4 {
		t.Fatalf("reply markup = %+v, want 4 session buttons", request.ReplyMarkup)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[0][0].Text); got != "➕ New Session" {
		t.Fatalf("first picker label = %q, want ➕ New Session", got)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[3][0].Text); got != "✖️ Cancel" {
		t.Fatalf("last picker label = %q, want ✖️ Cancel", got)
	}
}

func TestHandleUpdateRendersProviderAndPermissionsCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"binding": core.ClientBinding{Client: "telegram", ExternalKey: "42", SessionID: "session_1"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []core.Session{{
					ID:             "session_1",
					Title:          "docs",
					ProviderID:     "openai",
					ModelID:        "gpt-5.4",
					PermissionMode: core.PermissionModeAcceptEdits,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/setup/providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []map[string]any{
					{"id": "openai", "catalog_id": "openai", "name": "OpenAI", "configured": true, "active": true, "implemented": true, "model": "gpt-5.4"},
					{"id": "deepseek", "catalog_id": "deepseek", "name": "DeepSeek", "configured": true, "implemented": true, "model": "deepseek-v4"},
				},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	for _, tt := range []struct {
		text      string
		wantTitle string
		wantRows  int
	}{
		{text: "/provider", wantTitle: "Provider", wantRows: 4},
		{text: "/permissions", wantTitle: "Permission Mode", wantRows: 3},
	} {
		err := worker.handleUpdate(context.Background(), Update{
			UpdateID: 10,
			Message: &Message{
				MessageID: 10,
				Text:      tt.text,
				Chat:      Chat{ID: 42, Type: "private"},
				From:      &User{ID: 42},
			},
		})
		if err != nil {
			t.Fatalf("handleUpdate(%s) error = %v", tt.text, err)
		}
		if len(api.sendMessageRequests) == 0 {
			t.Fatalf("handleUpdate(%s) sent no messages", tt.text)
		}
		request := api.sendMessageRequests[len(api.sendMessageRequests)-1]
		if request.Text != tt.wantTitle {
			t.Fatalf("handleUpdate(%s) text = %q, want %q", tt.text, request.Text, tt.wantTitle)
		}
		if request.ReplyMarkup == nil || len(request.ReplyMarkup.InlineKeyboard) != tt.wantRows {
			t.Fatalf("handleUpdate(%s) reply markup = %+v, want %d rows", tt.text, request.ReplyMarkup, tt.wantRows)
		}
	}
}

func TestHandleUpdateContextCompactConfirmFlow(t *testing.T) {
	var compactCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"binding": core.ClientBinding{Client: "telegram", ExternalKey: "42", SessionID: "session_1"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []core.Session{{ID: "session_1", Title: "docs"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/session_1/compact":
			compactCalled = true
			_ = json.NewEncoder(w).Encode(core.SessionCompactResponse{
				Compact: core.CompactSessionResult{
					Message: core.Message{Content: "Context compacted."},
				},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 11,
		Message: &Message{
			MessageID: 11,
			Text:      "/context compact",
			Chat:      Chat{ID: 42, Type: "private"},
			From:      &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate(/context compact) error = %v", err)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	confirm := api.sendMessageRequests[0]
	if confirm.Text != "Compact context now?" {
		t.Fatalf("confirm text = %q", confirm.Text)
	}
	if confirm.ReplyMarkup == nil || len(confirm.ReplyMarkup.InlineKeyboard) != 1 {
		t.Fatalf("confirm markup = %+v", confirm.ReplyMarkup)
	}

	err = worker.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "cb-compact",
		From: &User{ID: 42},
		Message: &Message{
			MessageID: 77,
			Text:      confirm.Text,
			Chat:      Chat{ID: 42, Type: "private"},
		},
		Data: commandCallbackData("/context compact confirm"),
	})
	if err != nil {
		t.Fatalf("handleCallbackQuery(compact confirm) error = %v", err)
	}
	if !compactCalled {
		t.Fatal("compact endpoint was not called")
	}
	if len(api.editMessageRequests) < 2 {
		t.Fatalf("editMessageRequests len = %d, want progress and result edits", len(api.editMessageRequests))
	}
	if got := api.editMessageRequests[0].Text; got != compactProgressText {
		t.Fatalf("progress edit = %q, want %q", got, compactProgressText)
	}
	if got := api.editMessageRequests[len(api.editMessageRequests)-1].Text; got != "Context compacted." {
		t.Fatalf("result edit = %q, want compact result", got)
	}
}
