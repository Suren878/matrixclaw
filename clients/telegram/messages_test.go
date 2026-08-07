package telegram

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func TestUnsupportedDocumentImageIsStoredWithoutStartingRun(t *testing.T) {
	var saved localstorage.FileSaveRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/modules/storage/temp" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&saved); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(localstorage.TempFileResponse{File: localstorage.TempEntry{
			Path:     saved.Path,
			Title:    saved.Title,
			MIMEType: saved.MIMEType,
		}})
	}))
	defer server.Close()

	api := &documentImageBotAPI{content: []byte("<svg/>")}
	worker := &Worker{
		api: api,
		config: Config{
			BaseURL:          server.URL,
			ClientName:       "telegram-test",
			DaemonHTTPClient: server.Client(),
		},
	}
	err := worker.handleDocumentImageMessage(context.Background(), &Message{
		Chat: Chat{ID: 42, Type: "private"},
		Document: &Document{
			FileID:   "svg-file",
			FileName: "logo.svg",
			MIMEType: "image/svg+xml",
			FileSize: 6,
		},
	})
	if err != nil {
		t.Fatalf("handleDocumentImageMessage: %v", err)
	}
	if !strings.HasPrefix(saved.Path, "telegram/files/chat42-") || !strings.HasSuffix(saved.Path, "-logo.svg") {
		t.Fatalf("saved path = %q, want telegram/files path for logo.svg", saved.Path)
	}
	if saved.MIMEType != "image/svg+xml" {
		t.Fatalf("saved MIME type = %q, want image/svg+xml", saved.MIMEType)
	}
	if got := string(mustFileSaveContent(t, saved)); got != "<svg/>" {
		t.Fatalf("saved content = %q, want SVG content", got)
	}
	if len(api.messages) != 1 {
		t.Fatalf("Telegram replies = %d, want 1", len(api.messages))
	}
	reply := html.UnescapeString(api.messages[0].Text)
	for _, want := range []string{"can't open logo.svg", "JPEG, PNG, GIF, and WebP", "Temporary file saved: " + saved.Path} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply = %q, want %q", reply, want)
		}
	}
	if got := api.actionCount(); got != 0 {
		t.Fatalf("chat actions = %d, want 0 because no model run should start", got)
	}
}

func TestDocumentDownloadUsesTelegramFileSizeLimit(t *testing.T) {
	api := &oversizedDocumentBotAPI{}
	worker := &Worker{api: api}
	err := worker.handleDocumentMessage(context.Background(), &Message{
		Chat: Chat{ID: 42, Type: "private"},
		Document: &Document{
			FileID:   "large-file",
			FileName: "large.bin",
		},
	})
	if err != nil {
		t.Fatalf("handleDocumentMessage: %v", err)
	}
	if api.downloaded {
		t.Fatal("oversized Telegram file was downloaded")
	}
	if len(api.messages) != 1 || !strings.Contains(api.messages[0].Text, "File is too large") {
		t.Fatalf("Telegram replies = %#v, want a size-limit message", api.messages)
	}
}

func mustFileSaveContent(t *testing.T, request localstorage.FileSaveRequest) []byte {
	t.Helper()
	content, err := request.ContentBytes()
	if err != nil {
		t.Fatalf("decode saved content: %v", err)
	}
	return content
}

type documentImageBotAPI struct {
	recordingBotAPI
	content  []byte
	messages []SendMessageRequest
}

func (a *documentImageBotAPI) GetFile(context.Context, string) (File, error) {
	return File{FilePath: "documents/logo.svg"}, nil
}

func (a *documentImageBotAPI) DownloadFile(context.Context, string) ([]byte, error) {
	return a.content, nil
}

func (a *documentImageBotAPI) SendMessage(_ context.Context, request SendMessageRequest) (SentMessage, error) {
	a.messages = append(a.messages, request)
	return SentMessage{}, nil
}

type oversizedDocumentBotAPI struct {
	recordingBotAPI
	downloaded bool
	messages   []SendMessageRequest
}

func (a *oversizedDocumentBotAPI) GetFile(context.Context, string) (File, error) {
	return File{FilePath: "documents/large.bin", FileSize: maxTelegramStorageUploadBytes + 1}, nil
}

func (a *oversizedDocumentBotAPI) DownloadFile(context.Context, string) ([]byte, error) {
	a.downloaded = true
	return nil, nil
}

func (a *oversizedDocumentBotAPI) SendMessage(_ context.Context, request SendMessageRequest) (SentMessage, error) {
	a.messages = append(a.messages, request)
	return SentMessage{}, nil
}
