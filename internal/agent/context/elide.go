package agentcontext

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ElisionPercent of the usable window is where old bulky results get elided.
const ElisionPercent = 60

const (
	elideKeepRounds  = 5
	elideKeepReplies = 3
	elideMinTokens   = 1_000
	argsSummaryRunes = 80
)

// ElisionDue reports whether a prompt of tokens has reached the elision
// threshold of a usable window of limit tokens.
func ElisionDue(tokens, limit int) bool {
	return tokens >= limit*ElisionPercent/100
}

// Elision hides the tool results and images of messages up to these seqs from a
// request; the transcript keeps them.
type Elision struct {
	ResultsThroughSeq int64
	ImagesThroughSeq  int64
}

// NextElision covers the results before the last five tool rounds and the
// images before the last three replies of messages.
func NextElision(messages []transcript.Message) Elision {
	var rounds, replies []int64
	for _, message := range messages {
		if message.Role != transcript.MessageRoleAssistant || len(messageToolCallIDs(message)) > 0 {
			continue
		}
		replies = append(replies, message.Seq)
		if isToolStepReply(message) {
			rounds = append(rounds, message.Seq)
		}
	}
	var elision Elision
	if len(rounds) > elideKeepRounds {
		elision.ResultsThroughSeq = rounds[len(rounds)-elideKeepRounds] - 1
	}
	if len(replies) > elideKeepReplies {
		elision.ImagesThroughSeq = replies[len(replies)-elideKeepReplies] - 1
	}
	return elision
}

// Elide returns messages with the covered results over ~1k tokens and the
// covered images replaced by short notes; messages itself is not changed.
func Elide(messages []transcript.Message, elision Elision) []transcript.Message {
	if elision.ResultsThroughSeq == 0 && elision.ImagesThroughSeq == 0 {
		return messages
	}
	calls := map[string]transcript.ToolCallPart{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				calls[strings.TrimSpace(part.ToolCall.ID)] = *part.ToolCall
			}
		}
	}
	out := make([]transcript.Message, len(messages))
	for i, message := range messages {
		if message.Seq <= elision.ResultsThroughSeq && message.Role == transcript.MessageRoleTool {
			message = elideResults(message, calls)
		}
		if message.Seq <= elision.ImagesThroughSeq {
			message = elideImages(message)
		}
		out[i] = message
	}
	return out
}

func elideResults(message transcript.Message, calls map[string]transcript.ToolCallPart) transcript.Message {
	parts := slices.Clone(message.Parts)
	for i, part := range parts {
		if part.ToolResult == nil {
			continue
		}
		tokens := EstimateTextTokens(part.ToolResult.Content)
		if tokens <= elideMinTokens {
			continue
		}
		result := *part.ToolResult
		result.Content = elidedResultNote(calls[strings.TrimSpace(result.ToolCallID)], result, tokens)
		parts[i].ToolResult = &result
	}
	message.Parts = parts
	return message
}

func elidedResultNote(call transcript.ToolCallPart, result transcript.ToolResultPart, tokens int) string {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = strings.TrimSpace(result.Name)
	}
	note := fmt.Sprintf("[output of %s(%s) hidden, ~%s tokens; call again", name, argsSummary(call.Input), FormatShortNumber(tokens))
	if result.OutputPath != "" {
		note += " or read " + result.OutputPath
	}
	return note + " if needed]"
}

// argsSummary renders call arguments as short, sorted key=value pairs.
func argsSummary(input string) string {
	var args map[string]any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&args); err != nil {
		return trimRunes(strings.TrimSpace(input), argsSummaryRunes)
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value, _ := json.Marshal(args[key])
		pairs = append(pairs, key+"="+string(value))
	}
	return trimRunes(strings.Join(pairs, ", "), argsSummaryRunes)
}

func trimRunes(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}

func elideImages(message transcript.Message) transcript.Message {
	parts := make([]transcript.MessagePart, 0, len(message.Parts))
	var notes []string
	for _, part := range message.Parts {
		if part.Image != nil {
			notes = append(notes, "[image "+imagePartLabel(*part.Image)+" hidden to save context]")
			continue
		}
		parts = append(parts, part)
	}
	if len(notes) == 0 {
		return message
	}
	message.Parts = parts
	message.Content = strings.TrimSpace(strings.Join(append([]string{message.Content}, notes...), "\n\n"))
	return message
}
