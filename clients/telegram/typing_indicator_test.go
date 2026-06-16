package telegram

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunTypingIndicatorSendsForActiveStatusAndThrottles(t *testing.T) {
	api := &recordingBotAPI{}
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	worker := &Worker{
		api:    api,
		config: Config{ChatActionInterval: 4 * time.Second},
		now:    func() time.Time { return now },
	}
	target := chatTarget{chatID: 42, externalKey: "telegram:42"}
	run := &core.Run{ID: "run-1", Status: core.RunStatusRunning}

	worker.updateRunTypingIndicator(context.Background(), target, run)
	if got := api.actionCount(); got != 1 {
		t.Fatalf("typing actions after first active update = %d, want 1", got)
	}
	first := api.action(0)
	if first.ChatID != 42 || first.Action != "typing" {
		t.Fatalf("typing action = %+v, want chat 42 typing", first)
	}

	now = now.Add(2 * time.Second)
	worker.updateRunTypingIndicator(context.Background(), target, run)
	if got := api.actionCount(); got != 1 {
		t.Fatalf("typing actions before interval = %d, want 1", got)
	}

	now = now.Add(3 * time.Second)
	worker.updateRunTypingIndicator(context.Background(), target, run)
	if got := api.actionCount(); got != 2 {
		t.Fatalf("typing actions after interval = %d, want 2", got)
	}
}

func TestRunTypingIndicatorStopsForInactiveStatus(t *testing.T) {
	api := &recordingBotAPI{}
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	worker := &Worker{
		api:    api,
		config: Config{ChatActionInterval: 4 * time.Second},
		now:    func() time.Time { return now },
	}
	target := chatTarget{chatID: 42, externalKey: "telegram:42"}
	run := &core.Run{ID: "run-1", Status: core.RunStatusRunning}

	worker.updateRunTypingIndicator(context.Background(), target, run)
	run.Status = core.RunStatusWaitingApproval
	worker.updateRunTypingIndicator(context.Background(), target, run)
	run.Status = core.RunStatusRunning
	worker.updateRunTypingIndicator(context.Background(), target, run)

	if got := api.actionCount(); got != 2 {
		t.Fatalf("typing actions after inactive reset = %d, want 2", got)
	}
}

type recordingBotAPI struct {
	mu      sync.Mutex
	actions []SendChatActionRequest
}

func (a *recordingBotAPI) actionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.actions)
}

func (a *recordingBotAPI) action(index int) SendChatActionRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.actions[index]
}

func (a *recordingBotAPI) GetMe(context.Context) (User, error) { return User{}, nil }
func (a *recordingBotAPI) GetUpdates(context.Context, GetUpdatesRequest) ([]Update, error) {
	return nil, nil
}
func (a *recordingBotAPI) GetFile(context.Context, string) (File, error) { return File{}, nil }
func (a *recordingBotAPI) DownloadFile(context.Context, string) ([]byte, error) {
	return nil, nil
}
func (a *recordingBotAPI) SendMessage(context.Context, SendMessageRequest) (SentMessage, error) {
	return SentMessage{}, nil
}
func (a *recordingBotAPI) SendMessageDraft(context.Context, SendMessageDraftRequest) error {
	return nil
}
func (a *recordingBotAPI) SendChatAction(_ context.Context, req SendChatActionRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, req)
	return nil
}
func (a *recordingBotAPI) SendVoice(context.Context, SendVoiceRequest) (SentMessage, error) {
	return SentMessage{}, nil
}
func (a *recordingBotAPI) SendAudio(context.Context, SendAudioRequest) (SentMessage, error) {
	return SentMessage{}, nil
}
func (a *recordingBotAPI) SendDocument(context.Context, SendDocumentRequest) (SentMessage, error) {
	return SentMessage{}, nil
}
func (a *recordingBotAPI) EditMessageText(context.Context, EditMessageTextRequest) (EditMessageTextResponse, error) {
	return EditMessageTextResponse{}, nil
}
func (a *recordingBotAPI) EditMessageMedia(context.Context, EditMessageMediaRequest) (EditMessageMediaResponse, error) {
	return EditMessageMediaResponse{}, nil
}
func (a *recordingBotAPI) AnswerCallbackQuery(context.Context, AnswerCallbackQueryRequest) error {
	return nil
}
func (a *recordingBotAPI) AnswerGuestQuery(context.Context, AnswerGuestQueryRequest) (SentGuestMessage, error) {
	return SentGuestMessage{}, nil
}
func (a *recordingBotAPI) AnswerInlineQuery(context.Context, AnswerInlineQueryRequest) error {
	return nil
}
func (a *recordingBotAPI) DeleteMessage(context.Context, DeleteMessageRequest) error {
	return nil
}
func (a *recordingBotAPI) SetMyCommands(context.Context, SetMyCommandsRequest) error {
	return nil
}
func (a *recordingBotAPI) DeleteMyCommands(context.Context, DeleteMyCommandsRequest) error {
	return nil
}
