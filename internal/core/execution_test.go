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
