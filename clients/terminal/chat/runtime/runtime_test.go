package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func TestPrepareSendContentAddsTextAttachmentsToPrompt(t *testing.T) {
	content, err := prepareSendContent("fix this", []surfaceeditor.Attachment{{
		FilePath: "notes.txt",
		FileName: "notes.txt",
		MimeType: "text/plain",
		Content:  []byte("hello\nworld\n"),
	}})
	if err != nil {
		t.Fatalf("prepareSendContent() error = %v", err)
	}
	if !strings.Contains(content, "<system_info>") {
		t.Fatalf("prepared content missing system info: %q", content)
	}
	if !strings.Contains(content, "<file path='notes.txt'>") {
		t.Fatalf("prepared content missing file tag: %q", content)
	}
}

func TestPrepareSendContentRejectsUnsupportedBinaryAttachments(t *testing.T) {
	_, err := prepareSendContent("look", []surfaceeditor.Attachment{{
		FilePath: "archive.zip",
		FileName: "archive.zip",
		MimeType: "application/zip",
		Content:  []byte("zip"),
	}})
	if err == nil {
		t.Fatal("expected error for unsupported binary attachment")
	}
}

func TestRuntimeSendMessageTransformsTextAttachments(t *testing.T) {
	var gotText string
	var gotWorkingDir string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Text       string `json:"text"`
			WorkingDir string `json:"working_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotText = req.Text
		gotWorkingDir = req.WorkingDir
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": "session-1",
			"user_message": map[string]any{
				"id":         "msg-1",
				"session_id": "session-1",
				"role":       "user",
				"content":    req.Text,
				"created_at": "2026-04-22T10:00:00Z",
				"updated_at": "2026-04-22T10:00:00Z",
			},
			"run": map[string]any{
				"id":              "run-1",
				"session_id":      "session-1",
				"user_message_id": "msg-1",
				"status":          "accepted",
				"started_at":      "2026-04-22T10:00:00Z",
				"updated_at":      "2026-04-22T10:00:00Z",
			},
		})
	}))
	defer server.Close()

	rt := New(Config{
		BaseURL:     server.URL,
		ClientName:  "terminal:local",
		ExternalKey: "local",
		WorkingDir:  "/workspace/matrixclaw",
	})

	if _, err := rt.sendMessage(context.Background(), "session-1", "fix this", surfaceeditor.Attachment{
		FilePath: "notes.txt",
		FileName: "notes.txt",
		MimeType: "text/plain",
		Content:  []byte("hello"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if !strings.Contains(gotText, "<file path='notes.txt'>") {
		t.Fatalf("sent text missing attachment payload: %q", gotText)
	}
	if gotWorkingDir != "/workspace/matrixclaw" {
		t.Fatalf("sent working_dir = %q, want %q", gotWorkingDir, "/workspace/matrixclaw")
	}
}

func TestRuntimeSendMessageUploadsImageAttachments(t *testing.T) {
	var storageRequest localstorage.FileSaveRequest
	var messageRequest core.HandleMessageInput

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/storage/temp":
			if err := json.NewDecoder(r.Body).Decode(&storageRequest); err != nil {
				t.Fatalf("decode storage request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(localstorage.TempFileResponse{
				File: localstorage.TempEntry{
					Path:     "terminal/images/photo.png",
					Title:    "photo.png",
					MIMEType: "image/png",
					Size:     9,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(core.AcceptRunResult{
				SessionID: "session-1",
				Run:       core.Run{ID: "run-1"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := New(Config{
		BaseURL:     server.URL,
		ClientName:  "terminal:local",
		ExternalKey: "local",
		WorkingDir:  "/workspace/matrixclaw",
	})

	if _, err := rt.sendMessage(context.Background(), "session-1", "look", surfaceeditor.Attachment{
		FilePath: "photo.png",
		FileName: "photo.png",
		MimeType: "image/png",
		Content:  []byte("png-bytes"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if storageRequest.Path == "" || storageRequest.Title != "photo.png" || storageRequest.MIMEType != "image/png" || storageRequest.ContentBase64 == "" {
		t.Fatalf("storage request = %#v, want image upload", storageRequest)
	}
	if messageRequest.Text != "look" {
		t.Fatalf("message text = %q, want look", messageRequest.Text)
	}
	if len(messageRequest.Parts) != 2 || messageRequest.Parts[1].Image == nil {
		t.Fatalf("message parts = %#v, want text and image", messageRequest.Parts)
	}
	image := messageRequest.Parts[1].Image
	if image.StoragePath != "terminal/images/photo.png" || !image.Temporary || image.DataBase64 != "" {
		t.Fatalf("image part = %#v, want temporary storage reference", image)
	}
}

func TestImageAttachmentFileNameFallback(t *testing.T) {
	tests := []struct {
		name       string
		attachment surfaceeditor.Attachment
		want       string
	}{
		{
			name: "file name",
			attachment: surfaceeditor.Attachment{
				FileName: "photo.png",
			},
			want: "photo.png",
		},
		{
			name: "file path",
			attachment: surfaceeditor.Attachment{
				FilePath: "/tmp/screenshots/photo.png",
			},
			want: "photo.png",
		},
		{
			name:       "empty",
			attachment: surfaceeditor.Attachment{},
			want:       "image",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageAttachmentFileName(tt.attachment); got != tt.want {
				t.Fatalf("imageAttachmentFileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureSessionTreatsAny404BindingAsMissing(t *testing.T) {
	var useSessionCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bindings/current":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "no active binding for this client"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []map[string]any{{
					"id":         "session-1",
					"title":      "Session",
					"status":     "active",
					"created_at": "2026-04-22T10:00:00Z",
					"updated_at": "2026-04-22T10:00:00Z",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bindings/use":
			useSessionCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"binding": map[string]any{
					"client":       "terminal:local",
					"external_key": "local",
					"session_id":   "session-1",
					"updated_at":   "2026-04-22T10:00:00Z",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := New(Config{
		BaseURL:     server.URL,
		ClientName:  "terminal:local",
		ExternalKey: "local",
		WorkingDir:  "/workspace/matrixclaw",
	})

	sessionID, err := rt.ensureSession(context.Background())
	if err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	if sessionID != "session-1" {
		t.Fatalf("EnsureSession() sessionID = %q, want %q", sessionID, "session-1")
	}
	if useSessionCalls != 1 {
		t.Fatalf("UseSession calls = %d, want 1", useSessionCalls)
	}
}
