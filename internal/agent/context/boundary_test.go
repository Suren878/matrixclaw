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

// badCut describes why a tail starting at cut would split a reply from its
// calls or a call from its result; "" for a valid cut.
func badCut(messages []transcript.Message, cut int) string {
	if cut == 0 {
		return ""
	}
	if first := messages[cut]; first.Role == transcript.MessageRoleTool || len(messageToolCallIDs(first)) > 0 {
		return fmt.Sprintf("the tail starts at %s message %q", first.Role, first.ID)
	}
	calls, results := map[string]int{}, map[string]int{}
	for i, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				calls[part.ToolCall.ID] = i
			}
			if part.ToolResult != nil {
				results[part.ToolResult.ToolCallID] = i
			}
		}
	}
	for id, call := range calls {
		if result, ok := results[id]; ok && (call < cut) != (result < cut) {
			return fmt.Sprintf("call %q at %d and its result at %d", id, call, result)
		}
	}
	return ""
}

func TestTailStartNeverCutsBetweenACallAndItsResult(t *testing.T) {
	output := strings.Repeat("a", 4_000)
	step := func(seq int64, id string) (transcript.Message, transcript.Message, transcript.Message) {
		messages := toolStep(seq, id, output)
		messages[0].Content = "Checking " + id + "."
		return messages[0], messages[1], messages[2]
	}
	task := textMessage(1, transcript.MessageRoleUser, "r1", "task")
	reply, callA, resultA := step(2, "a")
	_, callB, resultB := step(5, "b")
	_, callC, resultC := step(8, "c")
	later, callD, resultD := step(20, "d")
	steer := textMessage(11, transcript.MessageRoleUser, "r1", "look at the parser too")
	note := transcript.Message{ID: "note", Seq: 12, Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngineModel, Content: "Context update: plan changed"}
	answer := textMessage(30, transcript.MessageRoleAssistant, "r1", strings.Repeat("b", 2_000))
	next := textMessage(31, transcript.MessageRoleUser, "r1", "next")
	_, unanswered, _ := step(40, "lost")
	_, _, orphan := step(50, "gone")
	layouts := map[string][]transcript.Message{
		"parallel results around a steer":  {task, reply, callA, callB, callC, resultA, steer, resultB, note, resultC, later, callD, resultD, answer},
		"call without a result, then more": {task, reply, callA, resultA, answer, next, later, unanswered, callD, resultD, answer},
		"result without a call":            {task, orphan, answer, next, answer},
	}
	for name, messages := range layouts {
		// The tail changes only where the budget crosses what a suffix needs.
		for i := range messages {
			for _, budget := range []int{EstimateMessageTokens(messages[i:]) - 1, EstimateMessageTokens(messages[i:])} {
				if bad := badCut(messages, TailStart(messages, budget)); bad != "" {
					t.Fatalf("%s, budget %d: %s", name, budget, bad)
				}
			}
		}
	}
	messages := layouts["call without a result, then more"]
	if got := TailStart(messages, EstimateMessageTokens(messages[10:])); got != 10 {
		t.Fatalf("TailStart = %d, want 10: a call whose result never came holds no cut back", got)
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

	got := Kept(&transcript.Compaction{RunID: "r1", Kept: []string{"User: first ask"}}, covered, []string{"r1"})
	want := []string{"User: first ask", "User: second ask", "User guidance: look at the logs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Kept = %q, want %q", got, want)
	}
	if got := Kept(&transcript.Compaction{RunID: "r0", Kept: []string{"User: old"}}, covered[:1], []string{"r1"}); !reflect.DeepEqual(got, []string{"User: second ask"}) {
		t.Fatalf("Kept across runs = %q", got)
	}
	continued := Kept(&transcript.Compaction{RunID: "r0", Kept: []string{"User: old"}}, covered[:2], []string{"r1", "r0"})
	if !reflect.DeepEqual(continued, []string{"User: old", "User: second ask", "User: another run"}) {
		t.Fatalf("Kept of a continuation = %q", continued)
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

func TestKeptCutsTheMiddleOfALongText(t *testing.T) {
	long := "HEAD" + strings.Repeat("m", 40_000) + "TAIL"
	kept := Kept(nil, []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", long)}, []string{"r1"})

	if len(kept) != 1 || !strings.HasPrefix(kept[0], "User: HEAD") || !strings.HasSuffix(kept[0], "TAIL") || !strings.Contains(kept[0], "tokens omitted ...]") {
		t.Fatalf("kept = %.60q ... %.60q", kept[0], kept[0][len(kept[0])-60:])
	}
	if tokens := EstimateTextTokens(kept[0]); tokens > 2_050 {
		t.Fatalf("kept text is ~%d tokens, want about 2k", tokens)
	}
}
