package agentcontext

import (
	"context"
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	minSummaryChunkTokens = 4_000
	summaryTextRunes      = 8_000
	summaryToolRunes      = 4_000
)

// summaryOutputTokens bounds a summary reply; a chunk smaller than it bounds
// the reply to its own size, so both fit the summarising window.
const summaryOutputTokens = 8_192

// ErrNothingToSummarise means the input holds no text a summary could keep.
var ErrNothingToSummarise = errors.New("nothing to summarise")

// Generator produces one model reply; agent.Model and providers.Runtime satisfy it.
type Generator interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

// SummaryInput is history summarised on its own, outside the conversation's own
// request; Previous is the summary it continues.
type SummaryInput struct {
	SessionID   string
	Previous    string
	Messages    []transcript.Message
	ChunkTokens int
}

// Summarize summarises the input in chunks of about ChunkTokens and merges the
// partial summaries, a chunk at a time, until one is left.
func Summarize(ctx context.Context, generator Generator, in SummaryInput) (string, error) {
	chunkTokens := max(in.ChunkTokens, minSummaryChunkTokens)
	chunks := summaryChunks(in.Previous, in.Messages, chunkTokens)
	if len(chunks) == 0 {
		return "", ErrNothingToSummarise
	}
	outputTokens := min(summaryOutputTokens, chunkTokens)
	partials, err := generateSummaries(ctx, generator, in.SessionID, outputTokens, "Summarise this part of a conversation:\n\n", chunks)
	for err == nil && len(partials) > 1 {
		chunks = packChunks(partials, chunkTokens, "\n\n---\n\n")
		if len(chunks) >= len(partials) {
			return "", errors.New("partial summaries are too long to merge")
		}
		partials, err = generateSummaries(ctx, generator, in.SessionID, outputTokens, "Merge these partial summaries of one conversation, oldest first, into a single summary:\n\n", chunks)
	}
	if err != nil {
		return "", err
	}
	return partials[0], nil
}

func generateSummaries(ctx context.Context, generator Generator, sessionID string, outputTokens int, instruction string, chunks []string) ([]string, error) {
	summaries := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		summary, err := generateSummary(ctx, generator, sessionID, outputTokens, instruction+chunk)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func generateSummary(ctx context.Context, generator Generator, sessionID string, outputTokens int, content string) (string, error) {
	response, err := generator.Generate(ctx, providers.Request{
		SessionID:       sessionID,
		SystemPrompt:    summarySystemPrompt(),
		Messages:        []providers.Message{{Role: string(transcript.MessageRoleUser), Content: content}},
		MaxOutputTokens: outputTokens,
	})
	if err != nil {
		return "", err
	}
	return SummaryReply(response)
}

// SummaryInstruction ends the run's own request when it asks for the summary
// that replaces the older part of the conversation.
const SummaryInstruction = "Summarise the conversation so far for yourself: the older messages will be replaced by this summary, the most recent ones stay visible. Do not call tools. Use these sections: Goal, Decisions, Files changed, Errors and fixes, Current state, Next step. Be concise and factual; leave out raw tool output, secrets and long code blocks."

// SummaryReply is the text of a summary reply; a cut, filtered or empty one fails.
func SummaryReply(response providers.Response) (string, error) {
	if err := stopReasonError(response); err != nil {
		return "", err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", errors.New("compact summary is empty")
	}
	return text, nil
}

func summarySystemPrompt() string {
	return strings.TrimSpace(`You compact matrixclaw chat histories into durable working context for future assistant turns.

Write a concise, factual summary with these sections:
Goal: what the user wants and the constraints they gave.
Decisions: choices already made and why.
Files changed: files, modules, commands and services changed or investigated.
Errors and fixes: failures seen and how they were resolved, or why they remain.
Current state: what is done and verified, what is in progress.
Next step: the immediate next action.

Leave out greetings, speculation, duplicated logs, raw tool dumps, secrets, API keys, OAuth tokens and long code or output blocks. Summarise a tool call together with its result. Reply in English.`)
}

// summaryChunks renders the previous summary and the message groups as text and
// packs them into chunks of about chunkTokens.
func summaryChunks(previous string, messages []transcript.Message, chunkTokens int) []string {
	var pieces []string
	if previous = strings.TrimSpace(previous); previous != "" {
		pieces = append(pieces, previous)
	}
	for _, group := range messageGroups(messages) {
		if text := groupText(group); text != "" {
			pieces = append(pieces, text)
		}
	}
	return packChunks(pieces, chunkTokens, "\n\n")
}

// packChunks joins pieces in order into chunks of about chunkTokens; a piece
// longer than a chunk is cut to one.
func packChunks(pieces []string, chunkTokens int, separator string) []string {
	var chunks []string
	var current strings.Builder
	used := 0
	for _, piece := range pieces {
		piece = trimToTokens(piece, chunkTokens)
		tokens := EstimateTextTokens(piece)
		if used > 0 && used+tokens > chunkTokens {
			chunks = append(chunks, current.String())
			current.Reset()
			used = 0
		}
		if used > 0 {
			current.WriteString(separator)
		}
		current.WriteString(piece)
		used += tokens
	}
	if used > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// trimToTokens keeps the start of text within about maxTokens.
func trimToTokens(text string, maxTokens int) string {
	if EstimateTextTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	return string(runes[:runesWithin(runes, maxTokens, false)]) + "…"
}

// messageGroup is a message and the results of the calls it made.
type messageGroup struct {
	messages []transcript.Message
}

// messageGroups groups each message with the results of its calls; system rows
// (engine notes, boundaries) are left out.
func messageGroups(messages []transcript.Message) []messageGroup {
	filtered := make([]transcript.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == transcript.MessageRoleSystem {
			continue
		}
		filtered = append(filtered, message)
	}
	groups := make([]messageGroup, 0, len(filtered))
	for i := 0; i < len(filtered); i++ {
		group := messageGroup{messages: []transcript.Message{filtered[i]}}
		ids := messageToolCallIDs(filtered[i])
		for len(ids) > 0 && i+1 < len(filtered) && messageIsToolResultFor(filtered[i+1], ids) {
			i++
			group.messages = append(group.messages, filtered[i])
		}
		groups = append(groups, group)
	}
	return groups
}

func groupText(group messageGroup) string {
	parts := make([]string, 0, len(group.messages))
	for _, message := range group.messages {
		role := strings.TrimSpace(string(message.Role))
		text := messageSummaryText(message)
		if role == "" || text == "" {
			continue
		}
		parts = append(parts, role+": "+text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func messageSummaryText(message transcript.Message) string {
	if len(message.Parts) == 0 {
		return trimRunes(message.Content, summaryTextRunes)
	}
	values := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch {
		case part.Text != nil:
			values = append(values, trimRunes(part.Text.Text, summaryTextRunes))
		case part.Image != nil:
			values = append(values, "image: "+imagePartLabel(*part.Image))
		case part.ToolCall != nil:
			values = append(values, "tool call: "+part.ToolCall.Name+" "+trimRunes(part.ToolCall.Input, summaryToolRunes))
		case part.ToolResult != nil:
			values = append(values, "tool result: "+part.ToolResult.Name+" "+trimRunes(part.ToolResult.Content, summaryToolRunes))
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n"))
}

func messageToolCallIDs(message transcript.Message) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, part := range message.Parts {
		if part.ToolCall == nil {
			continue
		}
		if id := strings.TrimSpace(part.ToolCall.ID); id != "" {
			ids[id] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func messageIsToolResultFor(message transcript.Message, ids map[string]struct{}) bool {
	if len(ids) == 0 || message.Role != transcript.MessageRoleTool {
		return false
	}
	for _, part := range message.Parts {
		if part.ToolResult == nil {
			continue
		}
		if _, ok := ids[strings.TrimSpace(part.ToolResult.ToolCallID)]; ok {
			return true
		}
	}
	return false
}
