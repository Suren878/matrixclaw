package telegram

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestWorker(t *testing.T, api BotAPI, daemonBaseURL string) *Worker {
	t.Helper()
	worker, err := NewWorker(Config{
		BaseURL:       daemonBaseURL,
		BotToken:      "test-token",
		AllowedUserID: 42,
		BotHTTPClient: api.(HTTPDoer),
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	worker.api = api
	return worker
}

func cleanButtonText(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\u00a0", ""))
}

type fakeBotAPI struct {
	mu                    sync.Mutex
	sendMessageErr        error
	editMessageErr        error
	nextMessageID         int64
	sendMessageRequests   []SendMessageRequest
	editMessageRequests   []EditMessageTextRequest
	deleteMessageRequests []DeleteMessageRequest
	answerRequests        []AnswerCallbackQueryRequest
	setCommandsRequests   []SetMyCommandsRequest
	file                  File
	fileContent           []byte
}

func (f *fakeBotAPI) Do(*http.Request) (*http.Response, error) {
	return nil, nil
}

func (f *fakeBotAPI) GetMe(context.Context) (User, error) {
	return User{ID: 1, IsBot: true, Username: "matrixclaw_bot"}, nil
}

func (f *fakeBotAPI) GetUpdates(context.Context, GetUpdatesRequest) ([]Update, error) {
	return nil, nil
}

func (f *fakeBotAPI) GetFile(context.Context, string) (File, error) {
	if f.file.FilePath == "" {
		return File{FileID: "file_1", FilePath: "documents/file.txt"}, nil
	}
	return f.file, nil
}

func (f *fakeBotAPI) DownloadFile(context.Context, string) ([]byte, error) {
	if f.fileContent == nil {
		return []byte("file content"), nil
	}
	return append([]byte(nil), f.fileContent...), nil
}

func (f *fakeBotAPI) SendMessage(_ context.Context, req SendMessageRequest) (SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextMessageID++
	f.sendMessageRequests = append(f.sendMessageRequests, req)
	if f.sendMessageErr != nil {
		err := f.sendMessageErr
		f.sendMessageErr = nil
		return SentMessage{}, err
	}
	return SentMessage{MessageID: f.nextMessageID}, nil
}

func (f *fakeBotAPI) SendChatAction(context.Context, SendChatActionRequest) error {
	return nil
}

func (f *fakeBotAPI) EditMessageText(_ context.Context, req EditMessageTextRequest) (EditMessageTextResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editMessageRequests = append(f.editMessageRequests, req)
	if f.editMessageErr != nil {
		err := f.editMessageErr
		f.editMessageErr = nil
		return EditMessageTextResponse{}, err
	}
	return EditMessageTextResponse{MessageID: req.MessageID}, nil
}

func (f *fakeBotAPI) AnswerCallbackQuery(_ context.Context, req AnswerCallbackQueryRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answerRequests = append(f.answerRequests, req)
	return nil
}

func (f *fakeBotAPI) DeleteMessage(_ context.Context, req DeleteMessageRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteMessageRequests = append(f.deleteMessageRequests, req)
	return nil
}

func (f *fakeBotAPI) SetMyCommands(_ context.Context, req SetMyCommandsRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCommandsRequests = append(f.setCommandsRequests, req)
	return nil
}

func (f *fakeBotAPI) DeleteMyCommands(context.Context, DeleteMyCommandsRequest) error {
	return nil
}

func TestSleepContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepContext(ctx, time.Second) {
		t.Fatal("sleepContext() = true, want false after cancellation")
	}
}
