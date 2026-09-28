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
	elideKeepPercent = 30
	elideKeepReplies = 3
	// elideRoundStep and elideReplyStep are how many more rounds or replies
	// must become eligible before an elision moves on.
	elideRoundStep   = 5
	elideReplyStep   = 5
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

// NextElision covers the results before the last five tool rounds, or before
// fewer of them when five outgrow 30% of the usable window of limit tokens (the
// newest round always stays), and the images before the last three replies.
func NextElision(messages []transcript.Message, limit int) Elision {
	var rounds, replies []int
	for i, message := range messages {
		if message.Role != transcript.MessageRoleAssistant || len(messageToolCallIDs(message)) > 0 {
			continue
		}
		replies = append(replies, i)
		if isToolStepReply(message) {
			rounds = append(rounds, i)
		}
	}
	var elision Elision
	if kept := keptRounds(messages, rounds, limit); len(rounds) > kept {
		elision.ResultsThroughSeq = messages[rounds[len(rounds)-kept]].Seq - 1
	}
	if len(replies) > elideKeepReplies {
		elision.ImagesThroughSeq = messages[replies[len(replies)-elideKeepReplies]].Seq - 1
	}
	return elision
}

// keptRounds is how many of the newest rounds, each starting at its index in
// messages, stay whole: up to five within their share of limit, at least one.
func keptRounds(messages []transcript.Message, rounds []int, limit int) int {
	budget, used, end := limit*elideKeepPercent/100, 0, len(messages)
	kept := 0
	for i := len(rounds) - 1; i >= 0 && kept < elideKeepRounds; i-- {
		used += EstimateMessageTokens(messages[rounds[i]:end])
		if kept > 0 && used > budget {
			break
		}
		kept, end = kept+1, rounds[i]
	}
	return max(kept, 1)
}

// AdvanceElision moves current up to NextElision of messages and reports
// whether it moved. Once results (or images) are elided, they move on only when
// five more tool rounds (or replies) became eligible, or when forced, so the
// request prefix stays stable; it never moves without hiding something more.
func AdvanceElision(messages []transcript.Message, limit int, current Elision, force bool) (Elision, bool) {
	next := NextElision(messages, limit)
	next.ResultsThroughSeq = max(next.ResultsThroughSeq, current.ResultsThroughSeq)
	next.ImagesThroughSeq = max(next.ImagesThroughSeq, current.ImagesThroughSeq)
	if next == current {
		return current, false
	}
	rounds, replies := newlyEligible(messages, current, next)
	results := next.ResultsThroughSeq > current.ResultsThroughSeq && (current.ResultsThroughSeq == 0 || rounds >= elideRoundStep)
	images := next.ImagesThroughSeq > current.ImagesThroughSeq && (current.ImagesThroughSeq == 0 || replies >= elideReplyStep)
	if !force && !results && !images || !hidesMore(messages, current, next) {
		return current, false
	}
	return next, true
}

// hidesMore reports whether next hides a result or an image current shows.
func hidesMore(messages []transcript.Message, current, next Elision) bool {
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Image != nil && message.Seq > current.ImagesThroughSeq && message.Seq <= next.ImagesThroughSeq {
				return true
			}
			if part.ToolResult != nil && message.Role == transcript.MessageRoleTool && message.Seq > current.ResultsThroughSeq && message.Seq <= next.ResultsThroughSeq &&
				EstimateTextTokens(part.ToolResult.Content) > elideMinTokens {
				return true
			}
		}
	}
	return false
}

// newlyEligible counts the tool rounds whose results and the replies whose
// images next covers beyond current.
func newlyEligible(messages []transcript.Message, current, next Elision) (rounds, replies int) {
	for _, message := range messages {
		if message.Role != transcript.MessageRoleAssistant || len(messageToolCallIDs(message)) > 0 {
			continue
		}
		if message.Seq > current.ImagesThroughSeq && message.Seq <= next.ImagesThroughSeq {
			replies++
		}
		if isToolStepReply(message) && message.Seq > current.ResultsThroughSeq && message.Seq <= next.ResultsThroughSeq {
			rounds++
		}
	}
	return rounds, replies
}

// Elide returns messages with the covered results over ~1k tokens and the
// covered images replaced by short notes; a result keeps the user guidance
// steered into it. messages itself is not changed.
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
		for _, guidance := range result.Guidance {
			result.Content += "\n\nUser guidance: " + guidance
		}
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
