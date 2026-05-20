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
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
)

func TestHandleUpdateDocumentImageSendsImagePart(t *testing.T) {
	var messageRequest core.HandleMessageInput
	var storageRequest localstorage.FileSaveRequest
	apiFileContent := []byte("webp-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/storage/temp":
			if err := json.NewDecoder(r.Body).Decode(&storageRequest); err != nil {
				t.Fatalf("decode storage request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(localstorage.TempFileResponse{
				File: localstorage.TempEntry{
					Path:     "telegram/images/diagram.webp",
					Title:    "diagram.webp",
					MIMEType: "image/webp",
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
		file:        File{FileID: "doc_img", FilePath: "documents/fallback.webp"},
		fileContent: apiFileContent,
	}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 12,
		Message: &Message{
			MessageID: 12,
			Caption:   "inspect this",
			Document: &Document{
				FileID:   "doc_img",
				FileName: "diagram.webp",
				MIMEType: "image/webp",
				FileSize: int64(len(apiFileContent)),
			},
			Chat: Chat{ID: 42, Type: "private"},
			From: &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if messageRequest.Text != "inspect this" {
		t.Fatalf("Text = %q, want caption", messageRequest.Text)
	}
	if len(messageRequest.Parts) != 2 || messageRequest.Parts[1].Image == nil {
		t.Fatalf("Parts = %#v, want text and image", messageRequest.Parts)
	}
	if !strings.HasPrefix(storageRequest.Path, "telegram/images/chat42-") || !strings.HasSuffix(storageRequest.Path, "-diagram.webp") {
		t.Fatalf("storage path = %q, want telegram image temp path", storageRequest.Path)
	}
	if storageRequest.Title != "diagram.webp" || storageRequest.MIMEType != "image/webp" {
		t.Fatalf("storage request = %#v, want document image metadata", storageRequest)
	}
	assertStorageContent(t, storageRequest, apiFileContent)
	assertTags(t, storageRequest.Tags, []string{"telegram", "temporary", "image"})

	image := messageRequest.Parts[1].Image
	if image.MIMEType != "image/webp" || image.Name != "diagram.webp" || image.StoragePath != "telegram/images/diagram.webp" || !image.Temporary || image.DataBase64 != "" {
		t.Fatalf("Image = %#v, want temporary storage reference", image)
	}
}

func TestHandleUpdateDocumentSavesTemporaryFile(t *testing.T) {
	var storageRequest localstorage.FileSaveRequest
	apiFileContent := []byte("plain text")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/storage/temp":
			if err := json.NewDecoder(r.Body).Decode(&storageRequest); err != nil {
				t.Fatalf("decode storage request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(localstorage.TempFileResponse{
				File: localstorage.TempEntry{
					Path:     storageRequest.Path,
					Title:    "report.txt",
					MIMEType: "text/plain",
					Size:     int64(len(apiFileContent)),
				},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{
		file:        File{FileID: "doc_text", FilePath: "documents/fallback.txt"},
		fileContent: apiFileContent,
	}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 13,
		Message: &Message{
			MessageID: 13,
			Document: &Document{
				FileID:   "doc_text",
				FileName: "report.txt",
				MIMEType: "text/plain",
				FileSize: int64(len(apiFileContent)),
			},
			Chat: Chat{ID: 42, Type: "private"},
			From: &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if !strings.HasPrefix(storageRequest.Path, "telegram/files/chat42-") || !strings.HasSuffix(storageRequest.Path, "-report.txt") || storageRequest.Title != "report.txt" || storageRequest.MIMEType != "text/plain" {
		t.Fatalf("storage request = %#v, want temporary document upload", storageRequest)
	}
	assertStorageContent(t, storageRequest, apiFileContent)
	assertTags(t, storageRequest.Tags, []string{"telegram", "temporary"})
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	if got := api.sendMessageRequests[0].Text; !strings.Contains(got, "Temporary file saved: "+storageRequest.Path) {
		t.Fatalf("reply text = %q, want saved file path", got)
	}
}

func TestHandleUpdateVoiceTranscribesAndSendsUserMessage(t *testing.T) {
	var sttRequest voicemodule.SpeechToTextRequest
	var messageRequest core.HandleMessageInput
	apiFileContent := []byte("ogg-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/voice/stt":
			if err := json.NewDecoder(r.Body).Decode(&sttRequest); err != nil {
				t.Fatalf("decode stt request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(voicemodule.SpeechToTextResponse{Text: "hello from voice"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&messageRequest); err != nil {
				t.Fatalf("decode message request: %v", err)
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
		file:        File{FileID: "voice_1", FilePath: "voice/file.ogg"},
		fileContent: apiFileContent,
	}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 15,
		Message: &Message{
			MessageID: 15,
			Voice: &Voice{
				FileID:   "voice_1",
				MIMEType: "audio/ogg",
				FileSize: int64(len(apiFileContent)),
			},
			Chat: Chat{ID: 42, Type: "private"},
			From: &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if got, err := sttRequest.ContentBytes(); err != nil || string(got) != string(apiFileContent) {
		t.Fatalf("stt content = %q/%v, want upload bytes", string(got), err)
	}
	if sttRequest.FileName != "file.ogg" || sttRequest.MIMEType != "audio/ogg" {
		t.Fatalf("stt request = %#v, want file metadata", sttRequest)
	}
	if messageRequest.Text != "hello from voice" {
		t.Fatalf("message text = %q, want transcript", messageRequest.Text)
	}
	if len(api.sendMessageRequests) == 0 || !strings.Contains(api.sendMessageRequests[0].Text, "Transcribed: hello from voice") {
		t.Fatalf("sendMessageRequests = %#v, want transcript notice", api.sendMessageRequests)
	}
}

func TestHandleUpdateTextToSpeechCommandSendsVoice(t *testing.T) {
	var ttsRequest voicemodule.TextToSpeechRequest
	var storageRequest localstorage.FileSaveRequest
	audio := []byte("mp3-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/voice/tts":
			if err := json.NewDecoder(r.Body).Decode(&ttsRequest); err != nil {
				t.Fatalf("decode tts request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(voicemodule.NewTextToSpeechResponse(audio, "audio/mpeg", "speech.mp3"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/storage/files":
			if err := json.NewDecoder(r.Body).Decode(&storageRequest); err != nil {
				t.Fatalf("decode storage request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(localstorage.FileResponse{
				File: localstorage.Entry{
					Path:     storageRequest.Path,
					Title:    storageRequest.Title,
					MIMEType: storageRequest.MIMEType,
					Size:     int64(len(audio)),
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
		UpdateID: 16,
		Message: &Message{
			MessageID: 16,
			Text:      "/tts say this",
			Chat:      Chat{ID: 42, Type: "private"},
			From:      &User{ID: 42},
		},
	})
	if err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}
	if ttsRequest.Text != "say this" || ttsRequest.VoiceID != "" || ttsRequest.Language != "" {
		t.Fatalf("tts request = %#v, want command text and daemon-selected defaults", ttsRequest)
	}
	if len(api.sendVoiceRequests) != 1 {
		t.Fatalf("sendVoiceRequests len = %d, want 1", len(api.sendVoiceRequests))
	}
	if string(api.sendVoiceRequests[0].Voice) != string(audio) || api.sendVoiceRequests[0].FileName != "speech.mp3" {
		t.Fatalf("send voice request = %#v, want generated audio", api.sendVoiceRequests[0])
	}
	if !strings.HasPrefix(storageRequest.Path, "telegram/audio/chat42-") || !strings.HasSuffix(storageRequest.Path, "-speech.mp3") {
		t.Fatalf("storage path = %q, want generated tts path", storageRequest.Path)
	}
	if storageRequest.Title != "speech.mp3" || storageRequest.MIMEType != "audio/mpeg" {
		t.Fatalf("storage request = %#v, want generated audio metadata", storageRequest)
	}
}

func TestHandleUpdateImageConflictRendersSharedSessionPicker(t *testing.T) {
	apiFileContent := []byte("png-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/modules/storage/temp":
			_ = json.NewEncoder(w).Encode(localstorage.TempFileResponse{
				File: localstorage.TempEntry{
					Path:     "telegram/images/photo.png",
					Title:    "photo.png",
					MIMEType: "image/png",
					Size:     int64(len(apiFileContent)),
				},
			})
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

	api := &fakeBotAPI{
		file:        File{FileID: "photo_big", FilePath: "photos/photo.png"},
		fileContent: apiFileContent,
	}
	worker := newTestWorker(t, api, server.URL)

	err := worker.handleUpdate(context.Background(), Update{
		UpdateID: 14,
		Message: &Message{
			MessageID: 14,
			Photo: []PhotoSize{
				{FileID: "photo_big", Width: 100, Height: 100, FileSize: int64(len(apiFileContent))},
			},
			Chat: Chat{ID: 42, Type: "private"},
			From: &User{ID: 42},
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
}

func assertStorageContent(t *testing.T, request localstorage.FileSaveRequest, want []byte) {
	t.Helper()
	got, err := request.ContentBytes()
	if err != nil {
		t.Fatalf("decode storage content: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("storage content = %q, want %q", string(got), string(want))
	}
}

func assertTags(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags = %#v, want %#v", got, want)
		}
	}
}
