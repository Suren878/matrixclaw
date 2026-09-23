package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func streamTestMessage(text string) transcript.Message {
	return transcript.Message{ID: "answer", RunID: "run", Role: transcript.MessageRoleAssistant, Content: text}
}

func TestPrivateAssistantDraftNotifiesOnlyOnFinalAnswer(t *testing.T) {
	api := &runRenderBotAPI{}
	now := time.Now()
	worker := &Worker{api: api, now: func() time.Time { return now }}
	state := newRunDeliveryState()
	target := chatTarget{chatID: 42}
	message := streamTestMessage("П")
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.sendCount() != 0 || api.draftCount() != 1 {
		t.Fatalf("first token sent persistent notification: %#v", api)
	}
	message.Content = "Полный ответ"
	now = now.Add(time.Second)
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.sendCount() != 0 || api.draftCount() != 2 {
		t.Fatal("draft update must not send a message")
	}
	if api.draftRequests[0].DraftID == 0 || api.draftRequests[0].DraftID != api.draftRequests[1].DraftID {
		t.Fatal("draft ID must stay stable")
	}
	message.Content = "Полный окончательный ответ."
	for i := 0; i < 2; i++ {
		if err := worker.renderAssistantUpdates(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
			t.Fatal(err)
		}
	}
	if api.sendCount() != 1 || api.messageText(1) != message.Content || api.messages[0].DisableNotification {
		t.Fatalf("incorrect final delivery: %#v", api.messages)
	}
	if api.draftCount() != 2 {
		t.Fatal("finalization must not send an empty Thinking draft")
	}
}

func TestGroupPreviewBuffersFirstCharacterAndThrottlesEdits(t *testing.T) {
	api := &runRenderBotAPI{}
	now := time.Now()
	worker := &Worker{api: api, now: func() time.Time { return now }}
	state := newRunDeliveryState()
	target := chatTarget{chatID: -42}
	message := streamTestMessage("Я")
	update := func() {
		t.Helper()
		if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
			t.Fatal(err)
		}
	}
	update()
	if api.sendCount() != 0 {
		t.Fatal("one character should remain buffered")
	}
	message.Content = strings.Repeat("я", 20)
	update()
	if api.sendCount() != 0 {
		t.Fatal("buffer threshold must count characters rather than UTF-8 bytes")
	}
	message.Content = "Первая законченная фраза."
	update()
	if api.sendCount() != 1 {
		t.Fatal("a complete phrase should be visible")
	}
	message.Content += " Продолжение."
	update()
	if api.editCount() != 0 {
		t.Fatal("unthrottled edit")
	}
	now = now.Add(time.Second)
	update()
	if api.editCount() != 1 || api.sendCount() != 1 {
		t.Fatal("preview must edit the existing message")
	}
}

func TestShortAnswerIsFlushedAtCompletionAndSlowPreviewIsBounded(t *testing.T) {
	api := &runRenderBotAPI{}
	now := time.Now()
	worker := &Worker{api: api, now: func() time.Time { return now }}
	state := newRunDeliveryState()
	target := chatTarget{chatID: -42}
	message := streamTestMessage("Да")
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if err := worker.renderAssistantUpdates(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.sendCount() != 1 || api.messageText(1) != "Да" {
		t.Fatal("short final answer was dropped")
	}
	state = newRunDeliveryState()
	message.ID = "slow"
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.sendCount() != 2 {
		t.Fatal("slow generation should not stay buffered forever")
	}
}

func TestDraftUnavailableFallsBackToOneBufferedMessage(t *testing.T) {
	api := &runRenderBotAPI{draftError: &APIError{ErrorCode: 400, Description: "drafts are not supported"}}
	worker := &Worker{api: api, config: Config{StreamFlushInterval: time.Nanosecond}}
	state := newRunDeliveryState()
	target := chatTarget{chatID: 42}
	message := streamTestMessage("Содержательная первая фраза.")
	for i := 0; i < 2; i++ {
		message.Content += " Продолжение."
		if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
			t.Fatal(err)
		}
	}
	if api.draftCount() != 1 || api.sendCount() != 1 || api.editCount() != 1 {
		t.Fatalf("fallback repeats sends: drafts=%d sends=%d edits=%d", api.draftCount(), api.sendCount(), api.editCount())
	}
}

func TestDraftFloodWaitDoesNotBlockDeliveryAndFinalTextStillArrives(t *testing.T) {
	api := &runRenderBotAPI{draftError: &APIError{ErrorCode: 429, RetryAfter: time.Minute}}
	now := time.Now()
	worker := &Worker{api: api, now: func() time.Time { return now }}
	state := newRunDeliveryState()
	target := chatTarget{chatID: 42}
	message := streamTestMessage("Partial")
	started := time.Now()
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err == nil {
		t.Fatal("expected flood response")
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("preview blocked on flood wait")
	}
	now = now.Add(time.Second)
	if err := worker.renderAssistantStreamUpdate(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.draftCount() != 1 {
		t.Fatal("preview ignored retry_after")
	}
	message.Content = "Complete final response."
	if err := worker.renderAssistantUpdates(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.messageText(1) != message.Content {
		t.Fatal("failed preview suppressed final response")
	}
}

func TestAssistantDeliveryRetainsSuccessfulChunksAfterFailure(t *testing.T) {
	api := &runRenderBotAPI{sendErrorAt: 2}
	worker := &Worker{api: api}
	state := newRunDeliveryState()
	target := chatTarget{chatID: 42}
	message := streamTestMessage(strings.Repeat("слово ", 900))
	if err := worker.renderAssistantUpdates(context.Background(), target, []transcript.Message{message}, "run", state); err == nil {
		t.Fatal("expected second chunk failure")
	}
	if len(state.assistant[message.ID].chunks) != 1 {
		t.Fatal("successful first chunk was forgotten")
	}
	if err := worker.renderAssistantUpdates(context.Background(), target, []transcript.Message{message}, "run", state); err != nil {
		t.Fatal(err)
	}
	if api.sendCount() != 2 || api.sendAttempts != 3 {
		t.Fatalf("duplicate delivery: sends=%d attempts=%d", api.sendCount(), api.sendAttempts)
	}
	if !api.messages[1].DisableNotification {
		t.Fatal("continuation chunk should be silent")
	}
}

func TestAssistantReplacesDeletedMessageAndRemovesStaleOverflow(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := &Worker{api: api}
	target := chatTarget{chatID: -42}
	sent, err := worker.sendAssistantMessage(context.Background(), target, sentAssistantMessage{}, strings.Repeat("a", 4500))
	if err != nil {
		t.Fatal(err)
	}
	api.editError = &APIError{ErrorCode: 400, Description: "message to edit not found"}
	api.deleteError = &APIError{ErrorCode: 400, Description: "message to delete not found"}
	sent, err = worker.sendAssistantMessage(context.Background(), target, sent, "Short final")
	if err != nil {
		t.Fatal(err)
	}
	if len(sent.chunks) != 1 || sent.chunks[0].messageID != 3 || len(api.deleted) != 1 || api.deleted[0] != 2 {
		t.Fatalf("stale chunks: sent=%#v deleted=%v", sent, api.deleted)
	}
}
