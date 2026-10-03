package telegram

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestEngineNotesAreSentOnceAndSilently(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := newWorker(Config{}, api)
	target := chatTarget{chatID: 7, externalKey: "7"}
	state := newRunDeliveryState()
	messages := []transcript.Message{
		{ID: "n1", RunID: "run-1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left."},
		{ID: "n2", RunID: "run-2", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "another run"},
		{ID: "s1", RunID: "run-1", Role: transcript.MessageRoleSystem, Content: "not an engine note"},
	}

	for i := 0; i < 2; i++ {
		if err := worker.renderEngineNotes(context.Background(), target, messages, "run-1", state); err != nil {
			t.Fatal(err)
		}
	}

	if api.sendCount() != 1 || api.messages[0].Text != "Note: Budget note: about 2 steps left." || !api.messages[0].DisableNotification {
		t.Fatalf("sent = %+v", api.messages)
	}
}

func TestModelOnlyEngineNoteIsNotSentWhileAShownOneIs(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := newWorker(Config{}, api)
	target := chatTarget{chatID: 7, externalKey: "7"}
	state := newRunDeliveryState()
	messages := []transcript.Message{
		{ID: "n1", RunID: "run-1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngineModel, Content: "Do not call tools. Reply briefly."},
		{ID: "n2", RunID: "run-1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left."},
	}

	if err := worker.renderEngineNotes(context.Background(), target, messages, "run-1", state); err != nil {
		t.Fatal(err)
	}

	if api.sendCount() != 1 || api.messages[0].Text != "Note: Budget note: about 2 steps left." {
		t.Fatalf("sent = %+v", api.messages)
	}
}

func TestRunStoppedEarlyOffersAContinueButtonOnce(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := newWorker(Config{}, api)
	target := chatTarget{chatID: 7, externalKey: "7"}
	state := newRunDeliveryState()
	stopped := core.Run{ID: "run-1", Status: core.RunStatusCompleted, StopReason: agent.StopBudgetExhausted}

	for i := 0; i < 2; i++ {
		if err := worker.offerContinue(context.Background(), target, stopped, state); err != nil {
			t.Fatal(err)
		}
	}
	finished := core.Run{ID: "run-2", Status: core.RunStatusCompleted, StopReason: agent.StopDone}
	if err := worker.offerContinue(context.Background(), target, finished, newRunDeliveryState()); err != nil {
		t.Fatal(err)
	}

	if api.sendCount() != 1 {
		t.Fatalf("sent = %+v, want one offer", api.messages)
	}
	markup, ok := api.messages[0].ReplyMarkup.(*InlineKeyboardMarkup)
	if !ok || len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 || markup.InlineKeyboard[0][0].CallbackData != commandCallbackData("/continue") {
		t.Fatalf("reply markup = %#v", api.messages[0].ReplyMarkup)
	}
}
