package agentcontext

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestEstimateCountsOtherScriptsDenserThanLatin(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{strings.Repeat("a", 400), 100},
		{strings.Repeat("я", 400), 160},
		{"abяя", 2},
		{"é", 1},
	} {
		if got := EstimateTextTokens(tc.text); got != tc.want {
			t.Errorf("EstimateTextTokens(%d runes of %q...) = %d, want %d", len([]rune(tc.text)), firstRune(tc.text), got, tc.want)
		}
	}
}

func firstRune(text string) string {
	for _, r := range text {
		return string(r)
	}
	return ""
}

func TestHeadTailKeepsBothEndsWithinTheLimit(t *testing.T) {
	for _, filler := range []string{"x", "ж"} {
		text := "HEAD" + strings.Repeat(filler, 100_000) + "TAIL"
		got := HeadTail(text, 8_000)
		if tokens := EstimateTextTokens(got); !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || !strings.Contains(got, "tokens omitted") || tokens > 8_000 || tokens < 7_900 {
			t.Fatalf("HeadTail of %q = %d tokens, want 7.9k to 8k: %.60q", filler, tokens, got)
		}
	}
	if got := HeadTail("short", 1_000); got != "short" {
		t.Fatalf("HeadTail of a short text = %q", got)
	}
}

func TestEstimatesCountReasoning(t *testing.T) {
	thinking := strings.Repeat("t", 4_000)
	message := transcript.Message{Role: transcript.MessageRoleAssistant, Content: "ok", Parts: append(transcript.NormalizeMessageParts("ok", nil),
		transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: thinking}},
		transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: thinking, Signature: "sig"}},
	)}
	if got := EstimateMessageTokens([]transcript.Message{message}); got < 2_000 {
		t.Fatalf("EstimateMessageTokens = %d, want both reasoning texts counted", got)
	}
	request := providers.Request{Messages: []providers.Message{{Role: "assistant", Content: "ok", Reasoning: []providers.ReasoningBlock{{Text: thinking, Signature: "sig"}}}}}
	if got := EstimateRequestTokens(request); got < 1_000 {
		t.Fatalf("EstimateRequestTokens = %d, want the signed reasoning counted", got)
	}
}
