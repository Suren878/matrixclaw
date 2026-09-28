package agenttest

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// SplitToolPair describes the first tool call of request not answered right
// after it, or the first result not answering a call just before it; "" when
// every call and result are paired.
func SplitToolPair(request providers.Request) string {
	open := map[string]bool{}
	for i, message := range request.Messages {
		if message.Role == string(transcript.MessageRoleTool) {
			if !open[message.ToolCallID] {
				return fmt.Sprintf("message %d answers %q, which no call just before it made", i, message.ToolCallID)
			}
			delete(open, message.ToolCallID)
			continue
		}
		if len(open) > 0 {
			return fmt.Sprintf("message %d follows unanswered calls %v", i, open)
		}
		for _, call := range message.ToolCalls {
			open[call.ID] = true
		}
	}
	if len(open) > 0 {
		return fmt.Sprintf("the request ends with unanswered calls %v", open)
	}
	return ""
}

// AssignmentCopies counts how often request carries the user's assignment
// verbatim: as its own message or kept verbatim in a summary.
func AssignmentCopies(request providers.Request, assignment string) int {
	copies := 0
	for _, message := range request.Messages {
		if message.Role == string(transcript.MessageRoleUser) && message.Content == assignment {
			copies++
		}
		copies += strings.Count(message.Content, "\n\nUser: "+assignment)
	}
	return copies
}

// SplitBoundary describes the first boundary of messages that covers a tool
// call without its result or a result without its call; "" when none does.
func SplitBoundary(messages []transcript.Message) string {
	callSeq, resultSeq := map[string]int64{}, map[string]int64{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				callSeq[part.ToolCall.ID] = message.Seq
			}
			if part.ToolResult != nil {
				resultSeq[part.ToolResult.ToolCallID] = message.Seq
			}
		}
	}
	for _, message := range messages {
		if message.Compaction == nil {
			continue
		}
		covers := message.Compaction.CoversThroughSeq
		for id, seq := range callSeq {
			if result, ok := resultSeq[id]; !ok || (seq <= covers) != (result <= covers) {
				return fmt.Sprintf("boundary %d covers through %d, call %q at %d, result at %d", message.Seq, covers, id, seq, result)
			}
		}
	}
	return ""
}
