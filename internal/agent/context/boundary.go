package agentcontext

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// TailPercent is the share of the usable window the newest turns keep verbatim
// after a summary.
const TailPercent = 25

// UnknownWindowTokens stands in for a model whose window is unknown.
const UnknownWindowTokens = 128_000

// EffectiveWindow is the prompt room of a model: its window without the output
// limit and a 5% reserve, never less than half the window.
func EffectiveWindow(windowTokens int, maxOutputTokens int) int {
	if windowTokens <= 0 {
		windowTokens = UnknownWindowTokens
	}
	return max(windowTokens-maxOutputTokens-windowTokens/20, windowTokens/2)
}

// TailStart is the index where the kept tail of messages begins: the newest
// whole turns within budget tokens, at least the newest one. A tail never starts
// at a call or a result, nor while a call before it waits for its result.
// 0 means nothing before the tail can be summarised.
func TailStart(messages []transcript.Message, budget int) int {
	cuts := cutPoints(messages)
	start, used := 0, 0
	for i := len(messages) - 1; i > 0; i-- {
		used += EstimateMessageTokens(messages[i : i+1])
		if !cuts[i] {
			continue
		}
		if used > budget && start > 0 {
			break
		}
		start = i
		if used > budget {
			break
		}
	}
	return start
}

// cutPoints marks where a tail may start; a call whose result never arrived
// does not hold a cut back.
func cutPoints(messages []transcript.Message) []bool {
	answered := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil {
				answered[strings.TrimSpace(part.ToolResult.ToolCallID)] = true
			}
		}
	}
	open := map[string]bool{}
	cuts := make([]bool, len(messages))
	for i, message := range messages {
		cuts[i] = len(open) == 0 && message.Role != transcript.MessageRoleTool && len(messageToolCallIDs(message)) == 0
		for _, part := range message.Parts {
			if part.ToolCall != nil && answered[strings.TrimSpace(part.ToolCall.ID)] {
				open[strings.TrimSpace(part.ToolCall.ID)] = true
			}
			if part.ToolResult != nil {
				delete(open, strings.TrimSpace(part.ToolResult.ToolCallID))
			}
		}
	}
	return cuts
}

// Kept is what a new boundary keeps verbatim: the run's user messages and the
// guidance steered into its tool results, after what the boundary it replaces
// kept for the same run.
func Kept(previous *transcript.Compaction, covered []transcript.Message, runID string) []string {
	runID = strings.TrimSpace(runID)
	var kept []string
	if previous != nil && previous.RunID == runID {
		kept = append(kept, previous.Kept...)
	}
	for _, message := range covered {
		if strings.TrimSpace(message.RunID) != runID {
			continue
		}
		if message.Role == transcript.MessageRoleUser {
			if text := strings.TrimSpace(message.Content); text != "" {
				kept = append(kept, "User: "+text)
			}
			continue
		}
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, guidance := range part.ToolResult.Guidance {
				kept = append(kept, "User guidance: "+guidance)
			}
		}
	}
	return kept
}

// SummaryText is the user text that stands for the history a boundary covers;
// empty for a /clear or no boundary.
func SummaryText(compaction *transcript.Compaction) string {
	if compaction == nil {
		return ""
	}
	summary := strings.TrimSpace(compaction.Summary)
	if summary == "" && len(compaction.Kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Summary of the earlier conversation, which it replaces:\n\n")
	b.WriteString(summary)
	if len(compaction.Kept) > 0 {
		b.WriteString("\n\nKept verbatim from that part:")
		for _, text := range compaction.Kept {
			b.WriteString("\n\n")
			b.WriteString(text)
		}
	}
	return b.String()
}

// SummaryMessages is the boundary as the first message of a request, when it
// has anything to say.
func SummaryMessages(compaction *transcript.Compaction) []providers.Message {
	text := SummaryText(compaction)
	if text == "" {
		return nil
	}
	return []providers.Message{{Role: string(transcript.MessageRoleUser), Content: text}}
}

// BoundaryLabel is the content clients show for a boundary message.
func BoundaryLabel(compaction transcript.Compaction) string {
	if compaction.Cleared {
		return "Context cleared."
	}
	return fmt.Sprintf("Context compacted: ~%s -> ~%s tokens.", FormatShortNumber(compaction.TokensBefore), FormatShortNumber(compaction.TokensAfter))
}
