package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRenderTelegramToolCallStatusShowsSearchQuery(t *testing.T) {
	text := renderTelegramToolCallStatus(core.ToolCallPart{
		ID:    "tool_1",
		Name:  "web_search",
		Input: `{"query":"amnezia vpn 2.0 release","limit":5}`,
	}, false, false, "")
	for _, want := range []string{"Searching web", "amnezia vpn 2.0 release"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool status missing %q: %q", want, text)
		}
	}
}

func TestRenderTelegramToolCallStatusShowsCompletionAndFailure(t *testing.T) {
	call := core.ToolCallPart{ID: "tool_1", Name: "web_fetch", Input: `{"url":"https://example.com"}`}
	completed := renderTelegramToolCallStatus(call, true, false, "")
	if !strings.Contains(completed, "Fetching page completed: https://example.com") {
		t.Fatalf("completed status = %q", completed)
	}
	failed := renderTelegramToolCallStatus(call, true, true, "network timeout")
	for _, want := range []string{"Fetching page failed: https://example.com", "network timeout"} {
		if !strings.Contains(failed, want) {
			t.Fatalf("failed status missing %q: %q", want, failed)
		}
	}
}

func TestRenderToolUpdatesSkipsTextToSpeechStatuses(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	worker := &Worker{api: api}
	target := chatTarget{kind: telegramTargetChat, chatID: 123, externalKey: "123"}
	state := newRunDeliveryState()
	messages := []core.Message{
		{
			ID:    "assistant_tool_call",
			RunID: "run_1",
			Role:  core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolCall,
				ToolCall: &core.ToolCallPart{
					ID:    "call_tts",
					Name:  "text_to_speech",
					Input: `{"text":"Test voice message. Hello, Suren."}`,
				},
			}},
		},
		{
			ID:    "tool_result",
			RunID: "run_1",
			Role:  core.MessageRoleTool,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolResult,
				ToolResult: &core.ToolResultPart{
					ToolCallID: "call_tts",
					Name:       "text_to_speech",
					Content:    "Speech audio generated.",
					Status:     "success",
				},
			}},
		},
	}

	if err := worker.renderToolCallUpdates(context.Background(), target, messages, "run_1", state); err != nil {
		t.Fatalf("renderToolCallUpdates: %v", err)
	}
	if err := worker.renderToolResultUpdates(context.Background(), target, messages, "run_1", state); err != nil {
		t.Fatalf("renderToolResultUpdates: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.messages) != 0 || len(api.edits) != 0 {
		t.Fatalf("messages=%#v edits=%#v, want no visible TTS status text", api.messages, api.edits)
	}
}

func TestRenderToolUpdatesSkipsWebStatuses(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	worker := &Worker{api: api}
	target := chatTarget{kind: telegramTargetChat, chatID: 123, externalKey: "123"}
	state := newRunDeliveryState()
	messages := []core.Message{
		{
			ID:    "assistant_tool_call",
			RunID: "run_1",
			Role:  core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolCall,
				ToolCall: &core.ToolCallPart{
					ID:    "call_search",
					Name:  "web_search",
					Input: `{"query":"restaurants near 59.763111,30.310916"}`,
				},
			}},
		},
		{
			ID:    "tool_result",
			RunID: "run_1",
			Role:  core.MessageRoleTool,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolResult,
				ToolResult: &core.ToolResultPart{
					ToolCallID: "call_search",
					Name:       "web_search",
					Content:    "Search results.",
					Status:     "success",
				},
			}},
		},
	}

	if err := worker.renderToolCallUpdates(context.Background(), target, messages, "run_1", state); err != nil {
		t.Fatalf("renderToolCallUpdates: %v", err)
	}
	if err := worker.renderToolResultUpdates(context.Background(), target, messages, "run_1", state); err != nil {
		t.Fatalf("renderToolResultUpdates: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.messages) != 0 || len(api.edits) != 0 {
		t.Fatalf("messages=%#v edits=%#v, want no visible web status text", api.messages, api.edits)
	}
}
