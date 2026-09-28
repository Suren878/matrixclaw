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

// wordyGenerator answers every request with a summary of about runes runes.
type wordyGenerator struct {
	runes    int
	requests []providers.Request
}

func (g *wordyGenerator) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	g.requests = append(g.requests, request)
	return providers.Response{Text: fmt.Sprintf("summary %d %s", len(g.requests), strings.Repeat("s", g.runes))}, nil
}

func TestSummarizeMergesLongPartialsInBoundedRounds(t *testing.T) {
	var messages []transcript.Message
	for i := 1; i <= 6; i++ {
		messages = append(messages, textMessage(int64(i), transcript.MessageRoleUser, "r1", strings.Repeat("w", 12_000)))
	}
	generator := &wordyGenerator{runes: 6_000}

	summary, err := Summarize(context.Background(), generator, SummaryInput{SessionID: "s1", Messages: messages, ChunkTokens: 4_000})

	if err != nil || len(generator.requests) != 12 || !strings.HasPrefix(summary, "summary 12 ") {
		t.Fatalf("summary = %.20q err = %v requests = %d, want 6 chunks, then 3, 2 and 1 merges", summary, err, len(generator.requests))
	}
	for i, request := range generator.requests {
		if tokens := EstimateTextTokens(request.Messages[0].Content); tokens > 4_100 {
			t.Fatalf("request %d carries ~%d tokens, want at most one chunk", i, tokens)
		}
	}
}

func TestSummarizeFailsWhenMergingDoesNotShrink(t *testing.T) {
	var messages []transcript.Message
	for i := 1; i <= 3; i++ {
		messages = append(messages, textMessage(int64(i), transcript.MessageRoleUser, "r1", strings.Repeat("w", 12_000)))
	}
	generator := &wordyGenerator{runes: 10_000}

	if _, err := Summarize(context.Background(), generator, SummaryInput{SessionID: "s1", Messages: messages, ChunkTokens: 4_000}); err == nil {
		t.Fatal("want an error when partial summaries are too long to merge")
	}
	if len(generator.requests) != 3 {
		t.Fatalf("requests = %d, want only the three chunk summaries", len(generator.requests))
	}
}

func TestAnOverlongPieceIsCutToAboutAChunkInAnyScript(t *testing.T) {
	for _, letter := range []string{"w", "ж"} {
		generator := &recordingGenerator{}

		_, err := Summarize(context.Background(), generator, SummaryInput{SessionID: "s1", Previous: strings.Repeat(letter, 100_000), ChunkTokens: 4_000})

		if err != nil || len(generator.requests) != 1 {
			t.Fatalf("%s: err = %v requests = %d", letter, err, len(generator.requests))
		}
		if tokens := EstimateTextTokens(generator.requests[0].Messages[0].Content); tokens < 3_900 || tokens > 4_100 {
			t.Fatalf("%s: the chunk is ~%d tokens, want about 4k", letter, tokens)
		}
	}
}
