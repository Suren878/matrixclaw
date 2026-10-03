package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestAssistantStreamEditsOnePersistentMessageAcrossApproval(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := newWorker(Config{}, api)
	worker.flushInterval = time.Nanosecond
	target := chatTarget{kind: telegramTargetChat, chatID: -42, externalKey: "telegram:-42"}
	state := newRunDeliveryState()
	state.assistant["assistant-before"] = sentAssistantMessage{firstSeenAt: time.Now().Add(-2 * time.Second)}
	state.assistant["assistant-after"] = sentAssistantMessage{firstSeenAt: time.Now().Add(-2 * time.Second)}
	runID := "run-1"
	messages := []transcript.Message{{
		ID:      "assistant-before",
		RunID:   runID,
		Role:    transcript.MessageRoleAssistant,
		Content: "Before",
	}}

	if err := worker.renderAssistantProgressUpdates(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("render initial assistant progress: %v", err)
	}
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("render initial assistant stream: %v", err)
	}
	if got := api.sendCount(); got != 1 {
		t.Fatalf("messages after first stream chunk = %d, want 1", got)
	}
	if got := api.messageText(1); got != "Before" {
		t.Fatalf("initial stream message = %q, want first text", got)
	}
	if api.messages[0].ReplyMarkup != nil {
		t.Fatalf("stream message reply markup = %#v, want nil so it remains editable", api.messages[0].ReplyMarkup)
	}

	messages[0].Content = "Before approval"
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("extend assistant stream: %v", err)
	}
	if got := api.sendCount(); got != 1 {
		t.Fatalf("messages after stream extension = %d, want the same message", got)
	}
	if got := api.editCount(); got != 1 {
		t.Fatalf("edits after stream extension = %d, want 1", got)
	}
	if got := api.messageText(1); got != "Before approval" {
		t.Fatalf("edited stream message = %q, want complete pre-approval text", got)
	}

	// A following tool call makes the streamed assistant text a completed
	// segment. Persisting it must reuse the same Telegram message.
	messages = append(messages, transcript.Message{
		ID:    "tool-call",
		RunID: runID,
		Role:  transcript.MessageRoleAssistant,
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindToolCall,
			ToolCall: &transcript.ToolCallPart{
				ID:       "call-1",
				Name:     "bash",
				Finished: true,
			},
		}},
	})
	if err := worker.renderAssistantProgressUpdates(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("persist completed assistant segment: %v", err)
	}
	if got := api.sendCount(); got != 1 {
		t.Fatalf("messages after segment completion = %d, want no duplicate", got)
	}

	approval := core.Approval{
		ID:       "approval-1",
		RunID:    runID,
		State:    core.ApprovalStatePending,
		ToolName: "bash",
	}
	if err := worker.renderApprovalUpdates(context.Background(), target, []core.Approval{approval}, runID, state); err != nil {
		t.Fatalf("render approval: %v", err)
	}
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("render stream while waiting: %v", err)
	}
	if got := api.sendCount(); got != 2 {
		t.Fatalf("messages while waiting approval = %d, want text and approval only", got)
	}

	messages = append(messages, transcript.Message{
		ID:      "assistant-after",
		RunID:   runID,
		Role:    transcript.MessageRoleAssistant,
		Content: "After approval",
	})
	if err := worker.renderAssistantProgressUpdates(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("render resumed assistant progress: %v", err)
	}
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("render resumed assistant stream: %v", err)
	}
	if err := worker.renderAssistantUpdates(context.Background(), target, messages, runID, state); err != nil {
		t.Fatalf("persist final assistant segment: %v", err)
	}

	if got := api.sendCount(); got != 3 {
		t.Fatalf("normal messages = %#v, want assistant/approval/assistant", api.messageTexts())
	}
	if got := api.messageText(1); got != "Before approval" {
		t.Errorf("first message = %q, want pre-approval assistant text", got)
	}
	if got := api.messageText(2); !strings.HasPrefix(got, "Approval required") {
		t.Errorf("second message = %q, want approval", got)
	}
	if got := api.messageText(3); got != "After approval" {
		t.Errorf("third message = %q, want post-approval assistant text", got)
	}
	if got := api.draftCount(); got != 0 {
		t.Errorf("live draft calls = %d, want 0", got)
	}
}

func TestAssistantStreamSplitsLongTextWithoutLosingContent(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := newWorker(Config{}, api)
	worker.flushInterval = time.Nanosecond
	target := chatTarget{kind: telegramTargetChat, chatID: -42, externalKey: "telegram:-42"}
	state := newRunDeliveryState()
	message := transcript.Message{
		ID:      "assistant-long",
		RunID:   "run-long",
		Role:    transcript.MessageRoleAssistant,
		Content: strings.Repeat("слово ", 900),
	}

	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, message.RunID, state); err != nil {
		t.Fatalf("render long assistant stream: %v", err)
	}
	if got := api.sendCount(); got != 2 {
		t.Fatalf("long stream messages = %d, want 2", got)
	}
	firstChunk := api.messageText(1)
	message.Content += strings.Repeat("ещё ", 100)
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, message.RunID, state); err != nil {
		t.Fatalf("extend long assistant stream: %v", err)
	}
	if got := api.sendCount(); got != 2 {
		t.Fatalf("extended long stream messages = %d, want existing chunks to be edited", got)
	}
	if got := api.messageText(1); got != firstChunk {
		t.Errorf("completed first chunk changed during later streaming")
	}
	if got := api.editCount(); got != 1 {
		t.Errorf("extended long stream edits = %d, want only the growing tail edited", got)
	}
	for index, text := range api.messageTexts() {
		if length := len([]rune(text)); length > defaultMessageLimit {
			t.Errorf("chunk %d length = %d, limit %d", index, length, defaultMessageLimit)
		}
	}
	joined := strings.Join(api.messageTexts(), " ")
	want := strings.TrimSpace(message.Content)
	if joined != want {
		t.Fatalf("joined chunks differ from original: got %d runes, want %d", len([]rune(joined)), len([]rune(want)))
	}
}

type runRenderBotAPI struct {
	recordingBotAPI
	messages      []SendMessageRequest
	edits         []EditMessageTextRequest
	drafts        int
	draftRequests []SendMessageDraftRequest
	draftError    error
	sendErrorAt   int
	sendAttempts  int
	editError     error
	deleted       []int64
	deleteError   error
}

func (a *runRenderBotAPI) SendMessage(_ context.Context, request SendMessageRequest) (SentMessage, error) {
	a.sendAttempts++
	if a.sendErrorAt == a.sendAttempts {
		return SentMessage{}, &APIError{ErrorCode: 429, Description: "Too Many Requests"}
	}
	a.messages = append(a.messages, request)
	return SentMessage{MessageID: int64(len(a.messages))}, nil
}

func (a *runRenderBotAPI) EditMessageText(_ context.Context, request EditMessageTextRequest) (EditMessageTextResponse, error) {
	if a.editError != nil {
		return EditMessageTextResponse{}, a.editError
	}
	a.edits = append(a.edits, request)
	index := int(request.MessageID) - 1
	if index >= 0 && index < len(a.messages) {
		a.messages[index].Text = request.Text
		a.messages[index].ParseMode = request.ParseMode
	}
	return EditMessageTextResponse{MessageID: request.MessageID}, nil
}

func (a *runRenderBotAPI) SendMessageDraft(_ context.Context, request SendMessageDraftRequest) error {
	a.drafts++
	a.draftRequests = append(a.draftRequests, request)
	return a.draftError
}

func (a *runRenderBotAPI) DeleteMessage(_ context.Context, request DeleteMessageRequest) error {
	a.deleted = append(a.deleted, request.MessageID)
	return a.deleteError
}

func (a *runRenderBotAPI) sendCount() int {
	return len(a.messages)
}

func (a *runRenderBotAPI) editCount() int {
	return len(a.edits)
}

func (a *runRenderBotAPI) draftCount() int {
	return a.drafts
}

func (a *runRenderBotAPI) messageText(messageID int64) string {
	index := int(messageID) - 1
	if index < 0 || index >= len(a.messages) {
		return ""
	}
	return a.messages[index].Text
}

func (a *runRenderBotAPI) messageTexts() []string {
	texts := make([]string, 0, len(a.messages))
	for _, message := range a.messages {
		texts = append(texts, message.Text)
	}
	return texts
}

func TestAgentStatusExplainsWhatTheSubagentDoes(t *testing.T) {
	line := func(input string) string {
		call := transcript.ToolCallPart{ID: "c1", Name: "agent", Input: input}
		return runningToolsLine([]transcript.Message{{Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &call}}}}, nil)
	}
	if got := line(`{"description":"Inspect the supervisor","prompt":"inspect the supervisor loop","runtime":"codex"}`); got != "Subagent is working: Inspect the supervisor" {
		t.Fatalf("line = %q", got)
	}
	if got := line(`{"description":"Scan","prompt":"scan","background":true}`); got != "Starting subagent: Scan" {
		t.Fatalf("background line = %q", got)
	}
}
