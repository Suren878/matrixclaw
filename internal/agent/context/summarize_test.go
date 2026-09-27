package agentcontext

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type recordingGenerator struct {
	requests []providers.Request
}

func (g *recordingGenerator) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	g.requests = append(g.requests, request)
	return providers.Response{Text: fmt.Sprintf("summary %d", len(g.requests))}, nil
}

func TestSummarizeChunksLongHistoryAndMergesThePartials(t *testing.T) {
	var messages []transcript.Message
	for i := 1; i <= 6; i++ {
		messages = append(messages, textMessage(int64(i), transcript.MessageRoleUser, "r1", strings.Repeat("w", 12_000)))
	}
	generator := &recordingGenerator{}

	summary, err := Summarize(context.Background(), generator, SummaryInput{SessionID: "s1", Previous: "EARLIER", Messages: messages, ChunkTokens: 4_000})

	if err != nil || summary != "summary 7" || len(generator.requests) != 7 {
		t.Fatalf("summary = %q err = %v requests = %d, want six chunks and one merge", summary, err, len(generator.requests))
	}
	for _, request := range generator.requests {
		if !strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			t.Fatalf("system prompt = %q", request.SystemPrompt)
		}
	}
	if first := generator.requests[0].Messages[0].Content; !strings.Contains(first, "EARLIER") {
		t.Fatalf("first chunk = %.80q, want the previous summary", first)
	}
	merge := generator.requests[6].Messages[0].Content
	if !strings.HasPrefix(merge, "Merge these partial summaries") || !strings.Contains(merge, "summary 1") || !strings.Contains(merge, "summary 6") {
		t.Fatalf("merge request = %.200q", merge)
	}
}
