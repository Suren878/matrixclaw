package agentcontext

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func textMessage(seq int64, role transcript.MessageRole, runID, text string) transcript.Message {
	return transcript.Message{ID: fmt.Sprintf("m%d", seq), Seq: seq, RunID: runID, Role: role, Content: text, Parts: transcript.NormalizeMessageParts(text, nil)}
}

// toolStep is one model response that called read once: reply, call, result.
func toolStep(seq int64, callID string, output string) []transcript.Message {
	reply := transcript.Message{ID: fmt.Sprintf("m%d", seq), Seq: seq, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}}}
	call := transcript.Message{ID: callID, Seq: seq + 1, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: callID, Name: "read", Input: "{}"}}}}
	result := transcript.Message{ID: callID + "_result", Seq: seq + 2, Role: transcript.MessageRoleTool, Content: output, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: callID, Name: "read", Content: output}}}}
	return []transcript.Message{reply, call, result}
}

func TestTailStartKeepsWholeTurnsWithinTheBudget(t *testing.T) {
	output := strings.Repeat("a", 4_000)
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	messages = append(messages, toolStep(2, "c1", output)...)
	messages = append(messages, toolStep(5, "c2", output)...)
	messages = append(messages, toolStep(8, "c3", output)...)

	if got := TailStart(messages, 2_100); got != 4 {
		t.Fatalf("TailStart = %d, want 4 (the last two tool steps)", got)
	}
	if got := TailStart(messages[:1], 2_100); got != 0 {
		t.Fatalf("TailStart of a single message = %d, want 0", got)
	}
}

func TestTailStartNeverSplitsACallFromItsResult(t *testing.T) {
	output := strings.Repeat("a", 4_000)
	reply := toolStep(2, "a", output)[0]
	callA, resultA := toolStep(2, "a", output)[1], toolStep(2, "a", output)[2]
	callB, resultB := toolStep(5, "b", output)[1], toolStep(5, "b", output)[2]
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task"), reply, callA, callB, resultA, resultB, textMessage(9, transcript.MessageRoleUser, "r1", "next")}

	if got := TailStart(messages, 1_500); got != 6 {
		t.Fatalf("TailStart = %d, want 6: no cut between parallel calls and their results", got)
	}
	if got := TailStart(messages, 1_000_000); got != 1 {
		t.Fatalf("TailStart with room for all = %d, want 1", got)
	}
}

func TestTailStartKeepsAtLeastTheNewestTurn(t *testing.T) {
	messages := []transcript.Message{
		textMessage(1, transcript.MessageRoleUser, "r1", "task"),
		textMessage(2, transcript.MessageRoleAssistant, "r1", strings.Repeat("b", 20_000)),
	}
	if got := TailStart(messages, 100); got != 1 {
		t.Fatalf("TailStart = %d, want 1", got)
	}
}

func TestKeptHoldsTheRunsUserTextAndSteersVerbatim(t *testing.T) {
	result := toolStep(3, "c1", "output")[2]
	result.RunID = "r1"
	result.Parts[0].ToolResult.Guidance = []string{"look at the logs"}
	covered := []transcript.Message{
		textMessage(1, transcript.MessageRoleUser, "r1", "second ask"),
		textMessage(2, transcript.MessageRoleUser, "r0", "another run"),
		result,
	}

	got := Kept(&transcript.Compaction{RunID: "r1", Kept: []string{"User: first ask"}}, covered, "r1")
	want := []string{"User: first ask", "User: second ask", "User guidance: look at the logs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Kept = %q, want %q", got, want)
	}
	if got := Kept(&transcript.Compaction{RunID: "r0", Kept: []string{"User: old"}}, covered[:1], "r1"); !reflect.DeepEqual(got, []string{"User: second ask"}) {
		t.Fatalf("Kept across runs = %q", got)
	}
}

func TestEffectiveWindowLeavesRoomForTheReplyAndAReserve(t *testing.T) {
	for _, tc := range []struct{ window, output, want int }{
		{200_000, 16_384, 173_616},
		{0, 16_384, 105_216},
		{20_000, 16_384, 10_000},
	} {
		if got := EffectiveWindow(tc.window, tc.output); got != tc.want {
			t.Errorf("EffectiveWindow(%d, %d) = %d, want %d", tc.window, tc.output, got, tc.want)
		}
	}
}

func TestSummaryTextCarriesTheSummaryAndTheKeptTexts(t *testing.T) {
	text := SummaryText(&transcript.Compaction{Summary: "Goal: ship.", Kept: []string{"User: do it"}})
	if !strings.Contains(text, "Goal: ship.") || !strings.Contains(text, "User: do it") {
		t.Fatalf("summary text = %q", text)
	}
	if SummaryText(&transcript.Compaction{Cleared: true}) != "" || SummaryText(nil) != "" {
		t.Fatal("a cleared or missing boundary has no summary text")
	}
}
