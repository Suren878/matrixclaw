package core

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildProviderConversationRepairsDanglingToolCalls(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_user_1",
			Role:    MessageRoleUser,
			Content: "change the file",
			Parts: []MessagePart{{
				Kind: MessagePartKindText,
				Text: &TextPart{Text: "change the file"},
			}},
		},
		{
			ID:   "call_1",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "call_1",
					Name:  "multiedit",
					Input: `{"file_path":"/tmp/test.txt","edits":[]}`,
				},
			}},
		},
		{
			ID:      "msg_user_2",
			Role:    MessageRoleUser,
			Content: "did it work?",
			Parts: []MessagePart{{
				Kind: MessagePartKindText,
				Text: &TextPart{Text: "did it work?"},
			}},
		},
	}

	conversation := buildProviderConversation(history)
	if len(conversation) != 4 {
		t.Fatalf("len(buildProviderConversation()) = %d, want 4", len(conversation))
	}
	if conversation[1].Role != string(MessageRoleAssistant) || len(conversation[1].ToolCalls) != 1 {
		t.Fatalf("conversation[1] = %#v, want assistant tool call", conversation[1])
	}
	if conversation[2].Role != string(MessageRoleTool) {
		t.Fatalf("conversation[2].Role = %q, want %q", conversation[2].Role, MessageRoleTool)
	}
	if conversation[2].ToolCallID != "call_1" {
		t.Fatalf("conversation[2].ToolCallID = %q, want %q", conversation[2].ToolCallID, "call_1")
	}
	if conversation[2].Content == "" {
		t.Fatal("synthetic tool repair message is empty")
	}
	if conversation[3].Role != string(MessageRoleUser) {
		t.Fatalf("conversation[3].Role = %q, want %q", conversation[3].Role, MessageRoleUser)
	}
}

func TestBuildProviderConversationSkipsInternalPlanRunPrompts(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_plan_run",
			Role:    MessageRoleUser,
			Content: "Execute the current session plan. Start with the first pending item.",
		},
		{
			ID:      "msg_user",
			Role:    MessageRoleUser,
			Content: "real user request",
		},
		{
			ID:      "msg_plan_update",
			Role:    MessageRoleUser,
			Content: "The session plan was updated. Continue the current work using this updated plan.",
		},
		{
			ID:      "msg_assistant",
			Role:    MessageRoleAssistant,
			Content: "done",
		},
	}

	conversation := buildProviderConversation(history)
	if len(conversation) != 2 {
		t.Fatalf("len(buildProviderConversation()) = %d, want 2: %#v", len(conversation), conversation)
	}
	if conversation[0].Role != string(MessageRoleUser) || conversation[0].Content != "real user request" {
		t.Fatalf("conversation[0] = %#v, want real user request", conversation[0])
	}
	if conversation[1].Role != string(MessageRoleAssistant) || conversation[1].Content != "done" {
		t.Fatalf("conversation[1] = %#v, want assistant response", conversation[1])
	}
}

func TestBuildProviderConversationKeepsCurrentInternalPlanRunPrompt(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_old_plan_run",
			RunID:   "run_old",
			Role:    MessageRoleUser,
			Content: "Execute the current session plan. Old run.",
		},
		{
			ID:      "msg_current_plan_run",
			RunID:   "run_current",
			Role:    MessageRoleUser,
			Content: "Execute the current session plan. Current run.",
		},
	}

	conversation, err := buildProviderConversationWithAttachmentsForRun(context.Background(), history, nil, "run_current")
	if err != nil {
		t.Fatalf("buildProviderConversationWithAttachmentsForRun() error = %v", err)
	}
	if len(conversation) != 1 {
		t.Fatalf("len(conversation) = %d, want current plan prompt only: %#v", len(conversation), conversation)
	}
	if conversation[0].Content != "Execute the current session plan. Current run." {
		t.Fatalf("conversation[0].Content = %q, want current plan prompt", conversation[0].Content)
	}
}

func TestBuildTextOnlyProviderConversationSkipsInternalPlanRunPrompts(t *testing.T) {
	t.Parallel()

	history := []Message{
		{Role: MessageRoleUser, Content: "Execute the current session plan. Start with the first pending item."},
		{Role: MessageRoleUser, Content: "real user request"},
		{Role: MessageRoleUser, Content: "The session plan was updated. Continue the current work using this updated plan."},
	}

	conversation := buildTextOnlyProviderConversation(history)
	if len(conversation) != 1 {
		t.Fatalf("len(buildTextOnlyProviderConversation()) = %d, want 1: %#v", len(conversation), conversation)
	}
	if conversation[0].Content != "real user request" {
		t.Fatalf("conversation[0].Content = %q, want real user request", conversation[0].Content)
	}
}

func TestBuildTextOnlyProviderConversationKeepsCurrentInternalPlanRunPrompt(t *testing.T) {
	t.Parallel()

	history := []Message{
		{RunID: "run_old", Role: MessageRoleUser, Content: "Execute the current session plan. Old run."},
		{RunID: "run_current", Role: MessageRoleUser, Content: "Execute the current session plan. Current run."},
	}

	conversation := buildTextOnlyProviderConversationForRun(history, "run_current")
	if len(conversation) != 1 {
		t.Fatalf("len(conversation) = %d, want current plan prompt only: %#v", len(conversation), conversation)
	}
	if conversation[0].Content != "Execute the current session plan. Current run." {
		t.Fatalf("conversation[0].Content = %q, want current plan prompt", conversation[0].Content)
	}
}

func TestBuildProviderConversationKeepsAssistantReasoningContent(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_user_1",
			Role:    MessageRoleUser,
			Content: "check it",
			Parts: []MessagePart{{
				Kind: MessagePartKindText,
				Text: &TextPart{Text: "check it"},
			}},
		},
		{
			ID:   "call_1",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{
				{
					Kind: MessagePartKindReasoning,
					Reasoning: &ReasoningPart{
						Text: "private thinking",
					},
				},
				{
					Kind: MessagePartKindToolCall,
					ToolCall: &ToolCallPart{
						ID:    "call_1",
						Name:  "read",
						Input: `{"file_path":"notes.txt"}`,
					},
				},
			},
		},
	}

	conversation := buildProviderConversation(history)
	if len(conversation) != 3 {
		t.Fatalf("len(conversation) = %d, want user, assistant tool call, synthetic tool result", len(conversation))
	}
	if conversation[1].ReasoningContent == nil || *conversation[1].ReasoningContent != "private thinking" {
		t.Fatalf("assistant reasoning = %#v, want private thinking", conversation[1].ReasoningContent)
	}
}

func TestExternalAssistantPartsPreserveInterleavedReasoningAndText(t *testing.T) {
	t.Parallel()

	assistant := &Message{}
	appendExternalReasoningDelta(assistant, "first")
	assistant.Content += "hello"
	appendExternalTextDelta(assistant, "hello")
	appendExternalReasoningDelta(assistant, " second")
	assistant.Content += " world"
	appendExternalTextDelta(assistant, " world")

	if len(assistant.Parts) != 4 {
		t.Fatalf("len(parts) = %d, want reasoning, text, reasoning, text sequence: %#v", len(assistant.Parts), assistant.Parts)
	}
	if assistant.Parts[0].Kind != MessagePartKindReasoning || assistant.Parts[0].Reasoning == nil || assistant.Parts[0].Reasoning.Text != "first" {
		t.Fatalf("first part = %#v, want first reasoning", assistant.Parts[0])
	}
	if assistant.Parts[1].Kind != MessagePartKindText || assistant.Parts[1].Text == nil || assistant.Parts[1].Text.Text != "hello" {
		t.Fatalf("second part = %#v, want hello text", assistant.Parts[1])
	}
	if assistant.Parts[2].Kind != MessagePartKindReasoning || assistant.Parts[2].Reasoning == nil || assistant.Parts[2].Reasoning.Text != " second" {
		t.Fatalf("third part = %#v, want second reasoning", assistant.Parts[2])
	}
	if assistant.Parts[3].Kind != MessagePartKindText || assistant.Parts[3].Text == nil || assistant.Parts[3].Text.Text != " world" {
		t.Fatalf("fourth part = %#v, want world text", assistant.Parts[3])
	}
}

func TestExternalAssistantPartsPreserveToolOrderBetweenTextDeltas(t *testing.T) {
	t.Parallel()

	assistant := &Message{}
	assistant.Content += "Checking the folder.\n"
	appendExternalTextDelta(assistant, "Checking the folder.\n")
	upsertExternalToolCall(assistant, "item_1", "bash", `{"command":"ls"}`, false)
	upsertExternalToolResult(assistant, "item_1", "bash", "file.txt\n", false, true)
	upsertExternalToolCall(assistant, "item_1", "bash", `{"command":"ls"}`, true)
	assistant.Content += "The folder contains file.txt.\n"
	appendExternalTextDelta(assistant, "The folder contains file.txt.\n")

	kinds := make([]MessagePartKind, len(assistant.Parts))
	for i, part := range assistant.Parts {
		kinds[i] = part.Kind
	}
	want := []MessagePartKind{
		MessagePartKindText,
		MessagePartKindToolCall,
		MessagePartKindToolResult,
		MessagePartKindText,
	}
	if len(kinds) != len(want) {
		t.Fatalf("parts = %#v, want kinds %v", assistant.Parts, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("parts kinds = %v, want %v", kinds, want)
		}
	}
	if assistant.Parts[0].Text.Text != "Checking the folder.\n" {
		t.Fatalf("before-tool text = %q", assistant.Parts[0].Text.Text)
	}
	if assistant.Parts[3].Text.Text != "The folder contains file.txt.\n" {
		t.Fatalf("after-tool text = %q", assistant.Parts[3].Text.Text)
	}
}

func TestExternalToolPartsPreserveCallInputAndStreamedOutput(t *testing.T) {
	t.Parallel()

	assistant := &Message{}
	upsertExternalToolCall(assistant, "item_1", "bash", `{"command":"ls"}`, false)
	upsertExternalToolResult(assistant, "item_1", "", "a", false, true)
	upsertExternalToolResult(assistant, "item_1", "", "b", false, true)
	upsertExternalToolCall(assistant, "item_1", "bash", `{"command":"ls"}`, true)

	if len(assistant.Parts) != 2 {
		t.Fatalf("len(parts) = %d, want tool call and result: %#v", len(assistant.Parts), assistant.Parts)
	}
	call := assistant.Parts[0].ToolCall
	if assistant.Parts[0].Kind != MessagePartKindToolCall || call == nil {
		t.Fatalf("first part = %#v, want tool call", assistant.Parts[0])
	}
	if call.ID != "item_1" || call.Name != "bash" || call.Input != `{"command":"ls"}` || !call.Finished {
		t.Fatalf("tool call = %#v, want finished bash call with input", call)
	}
	result := assistant.Parts[1].ToolResult
	if assistant.Parts[1].Kind != MessagePartKindToolResult || result == nil {
		t.Fatalf("second part = %#v, want tool result", assistant.Parts[1])
	}
	if result.ToolCallID != "item_1" || result.Name != "bash" || result.Content != "ab" || result.Status != "success" {
		t.Fatalf("tool result = %#v, want streamed bash result", result)
	}
}

func TestBuildProviderConversationBatchesConsecutiveToolCallsAndPairsResults(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_user_1",
			Role:    MessageRoleUser,
			Content: "edit all files",
			Parts: []MessagePart{{
				Kind: MessagePartKindText,
				Text: &TextPart{Text: "edit all files"},
			}},
		},
		{
			ID:   "call_1",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "call_1",
					Name:  "edit",
					Input: `{"file_path":"a.txt"}`,
				},
			}},
		},
		{
			ID:   "call_2",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "call_2",
					Name:  "edit",
					Input: `{"file_path":"b.txt"}`,
				},
			}},
		},
		{
			ID:      "result_1",
			Role:    MessageRoleTool,
			Content: "edited a",
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "call_1",
					Name:       "edit",
					Content:    "edited a",
				},
			}},
		},
		{
			ID:      "result_2",
			Role:    MessageRoleTool,
			Content: "edited b",
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "call_2",
					Name:       "edit",
					Content:    "edited b",
				},
			}},
		},
	}

	conversation := buildProviderConversation(history)
	if len(conversation) != 4 {
		t.Fatalf("len(buildProviderConversation()) = %d, want 4", len(conversation))
	}
	if conversation[1].Role != string(MessageRoleAssistant) || len(conversation[1].ToolCalls) != 2 {
		t.Fatalf("conversation[1] = %#v, want batched assistant tool calls", conversation[1])
	}
	if conversation[2].Role != string(MessageRoleTool) || conversation[2].ToolCallID != "call_1" {
		t.Fatalf("conversation[2] = %#v, want first tool result", conversation[2])
	}
	if conversation[3].Role != string(MessageRoleTool) || conversation[3].ToolCallID != "call_2" {
		t.Fatalf("conversation[3] = %#v, want second tool result", conversation[3])
	}
}

func TestBuildProviderConversationKeepsToolCallsProviderNeutral(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_user_1",
			Role:    MessageRoleUser,
			Content: "read these files",
			Parts: []MessagePart{{
				Kind: MessagePartKindText,
				Text: &TextPart{Text: "read these files"},
			}},
		},
		{
			ID:   "call_1",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "call_1",
					Name:  "read",
					Input: `{"file_path":"a"}{"file_path":"b"}`,
				},
			}},
		},
		{
			ID:      "result_1",
			Role:    MessageRoleTool,
			Content: "invalid arguments",
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "call_1",
					Name:       "read",
					Content:    "invalid arguments",
					IsError:    true,
				},
			}},
		},
		{
			ID:      "msg_user_2",
			Role:    MessageRoleUser,
			Content: "are you there?",
			Parts: []MessagePart{{
				Kind: MessagePartKindText,
				Text: &TextPart{Text: "are you there?"},
			}},
		},
	}

	conversation := buildProviderConversation(history)
	if len(conversation) != 4 {
		t.Fatalf("len(buildProviderConversation()) = %d, want 4", len(conversation))
	}
	if conversation[1].Role != string(MessageRoleAssistant) || len(conversation[1].ToolCalls) != 1 {
		t.Fatalf("conversation[1] = %#v, want raw assistant tool call", conversation[1])
	}
	if got := string(conversation[1].ToolCalls[0].Arguments); got != `{"file_path":"a"}{"file_path":"b"}` {
		t.Fatalf("tool arguments = %q, want raw invalid arguments", got)
	}
	if conversation[2].Role != string(MessageRoleTool) || conversation[2].ToolCallID != "call_1" {
		t.Fatalf("conversation[2] = %#v, want raw tool result", conversation[2])
	}
	if conversation[3].Role != string(MessageRoleUser) {
		t.Fatalf("conversation[3].Role = %q, want user", conversation[3].Role)
	}
}

func TestBuildProviderConversationResolvesStorageImage(t *testing.T) {
	t.Parallel()

	history := []Message{{
		ID:      "msg_user_1",
		Role:    MessageRoleUser,
		Content: "save this",
		Parts: []MessagePart{
			{Kind: MessagePartKindText, Text: &TextPart{Text: "save this"}},
			{Kind: MessagePartKindImage, Image: &ImagePart{
				MIMEType:    "image/png",
				Name:        "photo.png",
				StoragePath: "telegram/images/photo.png",
				Temporary:   true,
				Size:        9,
			}},
		},
	}}

	conversation, err := buildProviderConversationWithAttachments(context.Background(), history, fakeAttachmentReader{
		data: AttachmentData{
			Data:     []byte("png-bytes"),
			MIMEType: "image/png",
			Name:     "photo.png",
			Size:     9,
		},
	})
	if err != nil {
		t.Fatalf("buildProviderConversationWithAttachments() error = %v", err)
	}
	if len(conversation) != 1 || len(conversation[0].Images) != 1 {
		t.Fatalf("conversation = %#v, want one image", conversation)
	}
	image := conversation[0].Images[0]
	if image.DataBase64 != base64.StdEncoding.EncodeToString([]byte("png-bytes")) {
		t.Fatalf("image.DataBase64 = %q, want encoded bytes", image.DataBase64)
	}
	if !strings.Contains(conversation[0].Content, `temp_path="telegram/images/photo.png"`) {
		t.Fatalf("content = %q, want attachment temp_path", conversation[0].Content)
	}
}

type fakeAttachmentReader struct {
	data AttachmentData
}

func (r fakeAttachmentReader) ReadAttachment(context.Context, string, bool, int64) (AttachmentData, error) {
	return r.data, nil
}

func TestBuildTextOnlyProviderConversationPreservesToolHistoryAsText(t *testing.T) {
	t.Parallel()

	history := []Message{
		{
			ID:      "msg_user_1",
			Role:    MessageRoleUser,
			Content: "read this file",
		},
		{
			ID:   "call_1",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "call_1",
					Name:  "read",
					Input: `{"file_path":"a.txt"}`,
				},
			}},
		},
		{
			ID:      "result_1",
			Role:    MessageRoleTool,
			Content: "file contents",
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "call_1",
					Name:       "read",
					Content:    "file contents",
				},
			}},
		},
	}

	conversation := buildTextOnlyProviderConversation(history)
	if len(conversation) != 3 {
		t.Fatalf("len(buildTextOnlyProviderConversation()) = %d, want 3", len(conversation))
	}
	for _, message := range conversation {
		if len(message.ToolCalls) != 0 || strings.TrimSpace(message.ToolCallID) != "" || message.Role == string(MessageRoleTool) {
			t.Fatalf("conversation contains formal tool data: %#v", conversation)
		}
	}
	if conversation[1].Role != string(MessageRoleAssistant) || !strings.Contains(conversation[1].Content, "Previous tool call: read") || !strings.Contains(conversation[1].Content, `{"file_path":"a.txt"}`) {
		t.Fatalf("conversation[1] = %#v, want assistant text tool-call context", conversation[1])
	}
	if conversation[2].Role != string(MessageRoleUser) || !strings.Contains(conversation[2].Content, "Previous tool result from read:") || !strings.Contains(conversation[2].Content, "file contents") {
		t.Fatalf("conversation[2] = %#v, want user text tool-result context", conversation[2])
	}
}
