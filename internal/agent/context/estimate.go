package agentcontext

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// EstimatedImageTokens is the flat estimate for one image.
const EstimatedImageTokens = 1_500

const minimumCompactThreshold = 80_000

// Threshold is the token count at which a model window needs compaction.
func Threshold(windowTokens int) int {
	if windowTokens <= 0 {
		return 0
	}
	return max(windowTokens/2, minimumCompactThreshold)
}

// Recommendation reports whether a context of tokens should be compacted and why;
// windowTokens 0 means the window is unknown.
func Recommendation(tokens int, windowTokens int) (bool, string) {
	if windowTokens <= 0 {
		if tokens < minimumCompactThreshold {
			return false, ""
		}
		return true, "estimated context is high; compact session history before continuing"
	}
	if tokens < Threshold(windowTokens) {
		return false, ""
	}
	return true, "estimated context is high for the selected model; compact session history before continuing"
}

// SessionTokens estimates a session's context: fixed parts, the boundary's
// summary and the messages after it.
func SessionTokens(baseTokens int, compaction *transcript.Compaction, messages []transcript.Message) int {
	return baseTokens + EstimateTextTokens(SummaryText(compaction)) + EstimateMessageTokens(messages)
}

func EstimateMessageTokens(messages []transcript.Message) int {
	total := 0
	for _, message := range messages {
		messageTotal := 0
		for _, part := range message.Parts {
			switch part.Kind {
			case transcript.MessagePartKindText:
				if part.Text != nil {
					messageTotal += EstimateTextTokens(part.Text.Text)
				}
			case transcript.MessagePartKindImage:
				if part.Image != nil {
					messageTotal += EstimatedImageTokens
				}
			case transcript.MessagePartKindToolCall:
				if part.ToolCall != nil {
					messageTotal += EstimateTextTokens(part.ToolCall.Name)
					messageTotal += EstimateTextTokens(part.ToolCall.Input)
				}
			case transcript.MessagePartKindToolResult:
				if part.ToolResult != nil {
					messageTotal += EstimateTextTokens(part.ToolResult.Name)
					messageTotal += EstimateTextTokens(providerVisibleToolResultContent(part.ToolResult.Name, part.ToolResult.Content))
				}
			}
		}
		if messageTotal == 0 {
			messageTotal = EstimateTextTokens(message.Content)
		}
		total += messageTotal
	}
	return total
}

// EstimateTextTokens estimates runes/4 for Latin script and runes/2.5 for other
// scripts such as Cyrillic, which tokenizers split more finely.
func EstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	latin, other := 0, 0
	for _, r := range text {
		if r < latinScriptEnd {
			latin++
		} else {
			other++
		}
	}
	return max(1, (latin*10+other*16+39)/40)
}

// latinScriptEnd is the first rune after Latin Extended-B.
const latinScriptEnd = 0x0250

func EstimateRequestTokens(request providers.Request) int {
	total := EstimateTextTokens(request.SystemPrompt) + EstimateTextTokens(request.CustomInstructions)
	for _, message := range request.Messages {
		total += EstimateTextTokens(message.Role)
		total += EstimateTextTokens(message.Content)
		if message.ReasoningContent != nil {
			total += EstimateTextTokens(*message.ReasoningContent)
		}
		total += len(message.Images) * EstimatedImageTokens
		total += EstimateTextTokens(message.ToolCallID)
		for _, call := range message.ToolCalls {
			total += EstimateTextTokens(call.Name)
			total += EstimateTextTokens(string(call.Arguments))
		}
	}
	for _, tool := range request.Tools {
		total += EstimateTextTokens(tool.Name)
		total += EstimateTextTokens(tool.Description)
		total += EstimateTextTokens(string(tool.InputSchema))
	}
	return total
}

func FormatShortNumber(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 10_000:
		return fmt.Sprintf("%.0fk", float64(value)/1_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}
