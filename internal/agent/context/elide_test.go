package agentcontext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestNextElisionKeepsTheLastFiveRoundsAndThreeReplies(t *testing.T) {
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	var rounds []int64
	for i, seq := 0, int64(2); i < 7; i, seq = i+1, seq+3 {
		rounds = append(rounds, seq)
		messages = append(messages, toolStep(seq, fmt.Sprintf("c%d", i), "out")...)
	}

	if got, want := NextElision(messages, wideLimit), (Elision{ResultsThroughSeq: rounds[2] - 1, ImagesThroughSeq: rounds[4] - 1}); got != want {
		t.Fatalf("NextElision = %+v, want %+v", got, want)
	}
	if got := NextElision(messages[:16], wideLimit); got.ResultsThroughSeq != 0 {
		t.Fatalf("five rounds elide results through %d, want nothing", got.ResultsThroughSeq)
	}
}

// wideLimit is a usable window in which five rounds always fit.
const wideLimit = 1_000_000

func TestNextElisionKeepsFewerRoundsWhenTheyOutgrowTheirShare(t *testing.T) {
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	var rounds []int64
	for i, seq := 0, int64(2); i < 7; i, seq = i+1, seq+3 {
		rounds = append(rounds, seq)
		messages = append(messages, toolStep(seq, fmt.Sprintf("c%d", i), strings.Repeat("x", 8_000))...)
	}

	for _, tc := range []struct {
		limit int
		kept  int
	}{
		{limit: wideLimit, kept: 5},
		{limit: 15_000, kept: 2}, // 30% holds two rounds of ~2k tokens
		{limit: 5_000, kept: 1},  // the newest round stays even over its share
	} {
		if got, want := NextElision(messages, tc.limit).ResultsThroughSeq, rounds[len(rounds)-tc.kept]-1; got != want {
			t.Fatalf("limit %d: results elided through %d, want %d (%d rounds kept)", tc.limit, got, want, tc.kept)
		}
	}
}

func TestElideHidesOldBulkyResultsAndImagesOnly(t *testing.T) {
	big := strings.Repeat("x", 8_000)
	result := func(seq int64, callID, content, path string) transcript.Message {
		return transcript.Message{ID: callID + "_result", Seq: seq, Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: callID, Name: "read", Content: content, OutputPath: path}}}}
	}
	messages := []transcript.Message{
		{ID: "c1", Seq: 2, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "c1", Name: "read", Input: `{"path":"a.go","limit":20}`}}}},
		result(3, "c1", big, "/data/s1/out.txt"),
		result(4, "c2", "tiny", ""),
		{ID: "img", Seq: 5, Role: transcript.MessageRoleUser, Content: "look", Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindImage, Image: &transcript.ImagePart{Name: "shot.png", MIMEType: "image/png", StoragePath: "images/shot.png"}}}},
		result(9, "c3", big, ""),
	}

	out := Elide(messages, Elision{ResultsThroughSeq: 4, ImagesThroughSeq: 5})

	want := `[output of read(limit=20, path="a.go") hidden, ~2.0k tokens; call again or read /data/s1/out.txt if needed]`
	if got := out[1].Parts[0].ToolResult.Content; got != want {
		t.Fatalf("elided result = %q, want %q", got, want)
	}
	if out[2].Parts[0].ToolResult.Content != "tiny" || out[4].Parts[0].ToolResult.Content != big {
		t.Fatal("a small or newer result was elided")
	}
	if len(out[3].Parts) != 0 || !strings.Contains(out[3].Content, "look") || !strings.Contains(out[3].Content, "[image shot.png") {
		t.Fatalf("elided image message = %+v", out[3])
	}
	if messages[1].Parts[0].ToolResult.Content != big || len(messages[3].Parts) != 1 {
		t.Fatal("Elide changed the history it was given")
	}
}

func TestAdvanceElisionWaitsForFiveNewRoundsUnlessForced(t *testing.T) {
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	var rounds []int64
	for i, seq := 0, int64(2); i < 10; i, seq = i+1, seq+3 {
		rounds = append(rounds, seq)
		messages = append(messages, toolStep(seq, fmt.Sprintf("c%d", i), strings.Repeat("x", 8_000))...)
	}
	next := NextElision(messages, wideLimit)

	if got, moved := AdvanceElision(messages, wideLimit, Elision{}, false); !moved || got != next {
		t.Fatalf("first elision = %+v %v, want %+v", got, moved, next)
	}
	current := Elision{ResultsThroughSeq: rounds[1] - 1, ImagesThroughSeq: rounds[3] - 1}
	if got, moved := AdvanceElision(messages, wideLimit, current, false); moved || got != current {
		t.Fatalf("four new rounds moved the elision to %+v", got)
	}
	if got, moved := AdvanceElision(messages, wideLimit, current, true); !moved || got != next {
		t.Fatalf("forced elision = %+v %v, want %+v", got, moved, next)
	}
	current = Elision{ResultsThroughSeq: rounds[0] - 1, ImagesThroughSeq: rounds[2] - 1}
	if got, moved := AdvanceElision(messages, wideLimit, current, false); !moved || got != next {
		t.Fatalf("five new rounds = %+v %v, want %+v", got, moved, next)
	}
}

func TestElidedResultKeepsItsUserGuidance(t *testing.T) {
	content := strings.Repeat("x", 8_000) + "\n\nUser guidance: focus on the parser"
	messages := []transcript.Message{{ID: "c1_result", Seq: 3, Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult,
		ToolResult: &transcript.ToolResultPart{ToolCallID: "c1", Name: "read", Content: content, Guidance: []string{"focus on the parser"}}}}}}

	got := Elide(messages, Elision{ResultsThroughSeq: 3})[0].Parts[0].ToolResult.Content

	if !strings.HasPrefix(got, "[output of read") || !strings.HasSuffix(got, "]\n\nUser guidance: focus on the parser") {
		t.Fatalf("elided result = %q", got)
	}
}

func TestAdvanceElisionDoesNotMoveWithoutHidingAnything(t *testing.T) {
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	for i, seq := 0, int64(2); i < 10; i, seq = i+1, seq+3 {
		messages = append(messages, toolStep(seq, fmt.Sprintf("c%d", i), "out")...)
	}
	for _, force := range []bool{false, true} {
		if got, moved := AdvanceElision(messages, wideLimit, Elision{}, force); moved || got != (Elision{}) {
			t.Fatalf("forced %t: elision = %+v %v over small results and no images", force, got, moved)
		}
	}
}
