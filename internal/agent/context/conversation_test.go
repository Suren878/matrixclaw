package agentcontext

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestToProviderMessagesKeepsSVGAsAttachmentReference(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), transcript.Message{
		Role:    transcript.MessageRoleUser,
		Content: "Move this file.",
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindImage,
			Image: &transcript.ImagePart{
				MIMEType:    "image/svg+xml",
				Name:        "logo.svg",
				StoragePath: "telegram/images/logo.svg",
				Temporary:   true,
			},
		}},
	}, staticAttachmentReader{
		err: errors.New("storage file not found"),
	}, true)
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if len(messages[0].Images) != 0 {
		t.Fatalf("inline images = %d, want 0 for SVG", len(messages[0].Images))
	}
	if want := `temp_path="telegram/images/logo.svg"`; !strings.Contains(messages[0].Content, want) {
		t.Fatalf("content = %q, want attachment reference %q", messages[0].Content, want)
	}
}

func TestToProviderMessagesKeepsRasterImageInline(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), transcript.Message{
		Role:    transcript.MessageRoleUser,
		Content: "Describe this image.",
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindImage,
			Image: &transcript.ImagePart{
				MIMEType:    "image/png; charset=binary",
				StoragePath: "telegram/images/photo.png",
			},
		}},
	}, staticAttachmentReader{
		data: AttachmentData{MIMEType: "image/png", Data: []byte("png")},
	}, true)
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 || len(messages[0].Images) != 1 {
		t.Fatalf("messages = %#v, want one message with one inline image", messages)
	}
}

func TestToProviderMessagesDoesNotFailForExpiredRasterImage(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), transcript.Message{
		Role: transcript.MessageRoleUser,
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindImage,
			Image: &transcript.ImagePart{
				MIMEType:    "image/png",
				Name:        "expired.png",
				StoragePath: "telegram/images/expired.png",
				Temporary:   true,
			},
		}},
	}, staticAttachmentReader{err: fmt.Errorf("%w: storage file not found", ErrAttachmentUnavailable)}, true)
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if len(messages[0].Images) != 0 {
		t.Fatalf("inline images = %d, want 0", len(messages[0].Images))
	}
	for _, want := range []string{`temp_path="telegram/images/expired.png"`, "Unavailable attachments:", "file is no longer available"} {
		if !strings.Contains(messages[0].Content, want) {
			t.Errorf("content = %q, want %q", messages[0].Content, want)
		}
	}
}

func TestToProviderMessagesDoesNotReadImageForTextOnlyModel(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), transcript.Message{
		Role:    transcript.MessageRoleUser,
		Content: "Describe this image.",
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindImage,
			Image: &transcript.ImagePart{
				MIMEType:    "image/png",
				Name:        "photo.png",
				StoragePath: "telegram/images/photo.png",
			},
		}},
	}, staticAttachmentReader{err: errors.New("reader must not be called")}, false)
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if len(messages[0].Images) != 0 {
		t.Fatalf("inline images = %d, want 0", len(messages[0].Images))
	}
	if want := "selected model does not support image input"; !strings.Contains(messages[0].Content, want) {
		t.Fatalf("content = %q, want %q", messages[0].Content, want)
	}
}

type staticAttachmentReader struct {
	data AttachmentData
	err  error
}

func TestProviderConversationPairsResultsWithMixedTextAndToolCalls(t *testing.T) {
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect it"},
		{Role: transcript.MessageRoleAssistant, Content: "Checking now", Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "call-1", Name: "inspect", Input: `{}`}}}},
		{Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: "call-1", Name: "inspect", Content: "Actual result"}}}},
		{Role: transcript.MessageRoleAssistant, Content: "Done"},
	}
	conversation, err := Conversation(context.Background(), history, nil, "", false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 4 || conversation[1].Content != "Checking now" || conversation[2].ToolCallID != "call-1" || conversation[2].Content != "Actual result" {
		t.Fatalf("conversation lost or reordered tool result: %#v", conversation)
	}
}

func TestUnansweredCallsOfAnEndedRunGetAClosingResult(t *testing.T) {
	callMessage := func(id string, deferred bool) transcript.Message {
		return transcript.Message{ID: id, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: "write", Input: `{}`, Deferred: deferred}}}}
	}
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Change both"},
		callMessage("started", false),
		callMessage("held", true),
		{Role: transcript.MessageRoleUser, Content: "Never mind"},
	}
	conversation, err := Conversation(context.Background(), history, nil, "", false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]string{}
	for _, message := range conversation {
		if message.ToolCallID != "" {
			results[message.ToolCallID] = message.Content
		}
	}
	if results["started"] != "Tool execution failed before completion." || results["held"] != "Not run: the run was canceled." {
		t.Fatalf("results = %v", results)
	}
}

func (r staticAttachmentReader) ReadAttachment(context.Context, string, bool, int64) (AttachmentData, error) {
	return r.data, r.err
}

func TestOversizedResultReachesTheModelAsHeadAndTail(t *testing.T) {
	big := strings.Repeat("r", 200_000)
	history := []transcript.Message{
		{ID: "c1", Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "c1", Name: "browser_snapshot", Input: "{}"}}}},
		{ID: "c1_result", Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: "c1", Name: "browser_snapshot", Content: big}}}},
	}
	messages, err := Conversation(context.Background(), history, nil, "run", false, Identity{})
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %d err = %v", len(messages), err)
	}
	content := messages[1].Content
	if EstimateTextTokens(content) > LargeOutputTokens || !strings.Contains(content, "tokens omitted") || EstimateTextTokens(content) < 4_000 {
		t.Fatalf("tool content = %d tokens", EstimateTextTokens(content))
	}
}
