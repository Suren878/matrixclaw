package core

import (
	"context"
	"strings"
	"testing"
)

func TestProviderConversationCompactsLargeBrowserSnapshotResult(t *testing.T) {
	largeSnapshot := "### Page\n- Page URL: https://example.com\n### Snapshot\n" + strings.Repeat("- generic [ref=e1]: repeated browser node\n", 300) + "\nimportant tail value"
	history := []Message{
		{
			ID:   "call1",
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "call1",
					Name:  "mcp_browser_browser_snapshot",
					Input: `{"depth":8}`,
				},
			}},
		},
		{
			ID:   "result1",
			Role: MessageRoleTool,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "call1",
					Name:       "mcp_browser_browser_snapshot",
					Content:    largeSnapshot,
					Status:     "success",
				},
			}},
		},
	}

	conversation, err := buildProviderConversationWithAttachmentsForRun(context.Background(), history, nil, "")
	if err != nil {
		t.Fatalf("buildProviderConversationWithAttachmentsForRun() error = %v", err)
	}
	if len(conversation) != 2 {
		t.Fatalf("conversation length = %d, want 2", len(conversation))
	}
	got := conversation[1].Content
	if len(got) >= len(largeSnapshot) {
		t.Fatalf("provider tool result length = %d, want less than raw snapshot length %d", len(got), len(largeSnapshot))
	}
	if !strings.Contains(got, providerToolResultTruncationNotice) {
		t.Fatalf("provider tool result missing truncation notice:\n%s", got)
	}
	if !strings.Contains(got, "important tail value") {
		t.Fatalf("provider tool result should retain tail context:\n%s", got)
	}
}

func TestEstimateMessageTokensUsesProviderVisibleToolResultContent(t *testing.T) {
	largeSnapshot := "### Snapshot\n" + strings.Repeat("- generic [ref=e1]: repeated browser node\n", 500)
	messages := []Message{{
		ID:   "result1",
		Role: MessageRoleTool,
		Parts: []MessagePart{{
			Kind: MessagePartKindToolResult,
			ToolResult: &ToolResultPart{
				ToolCallID: "call1",
				Name:       "mcp_browser_browser_snapshot",
				Content:    largeSnapshot,
			},
		}},
	}}

	rawTokens := EstimateTextTokens(largeSnapshot)
	estimated := EstimateMessageTokens(messages)
	if estimated >= rawTokens {
		t.Fatalf("EstimateMessageTokens = %d, want less than raw snapshot tokens %d", estimated, rawTokens)
	}
}
