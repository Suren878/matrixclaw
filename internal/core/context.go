package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const compactMessagePrefix = "🧠 Context compacted"
const contextClearedMessagePrefix = "🧹 Context cleared"
const contextClearedSummary = "Context cleared by user."
const maxCompactPromptRunes = 80_000
const EstimatedImageTokens = 1_500
const compactBackoffMinimumSavingsPercent = 10

type CompactSessionResult struct {
	Message Message       `json:"message"`
	Context ContextReport `json:"context"`
}

type ContextBlockKind string

const (
	ContextBlockSystemPrompt       ContextBlockKind = "system_prompt"
	ContextBlockCustomInstructions ContextBlockKind = "custom_instructions"
	ContextBlockCompactSummary     ContextBlockKind = "compact_summary"
	ContextBlockClearMarker        ContextBlockKind = "clear_marker"
	ContextBlockMessages           ContextBlockKind = "messages"
	ContextBlockToolSchemas        ContextBlockKind = "tool_schemas"
)

type ContextBlock struct {
	ID             string           `json:"id"`
	Kind           ContextBlockKind `json:"kind"`
	Source         string           `json:"source"`
	TokenEstimate  int              `json:"token_estimate"`
	Included       bool             `json:"included"`
	Truncated      bool             `json:"truncated,omitempty"`
	CacheStability string           `json:"cache_stability,omitempty"`
}

type ContextReport struct {
	SessionID         string         `json:"session_id"`
	Estimated         bool           `json:"estimated"`
	TokenEstimate     int            `json:"token_estimate"`
	WindowTokens      int            `json:"window_tokens,omitempty"`
	MessageCount      int            `json:"message_count"`
	Blocks            []ContextBlock `json:"blocks"`
	LastProviderUsage *ProviderUsage `json:"last_provider_usage,omitempty"`
	Compact           ContextCompact `json:"compact"`
}

type ProviderUsage struct {
	InputTokens     int64           `json:"input_tokens,omitempty"`
	OutputTokens    int64           `json:"output_tokens,omitempty"`
	TotalTokens     int64           `json:"total_tokens,omitempty"`
	CachedTokens    int64           `json:"cached_tokens,omitempty"`
	ReasoningTokens int64           `json:"reasoning_tokens,omitempty"`
	Estimated       bool            `json:"estimated,omitempty"`
	ProviderRaw     json.RawMessage `json:"provider_raw,omitempty"`
}

type ContextCompact struct {
	Recommended bool   `json:"recommended"`
	Reason      string `json:"reason,omitempty"`
}

var ErrSessionRequired = errors.New("session_id is required")

func ContextClearedMessageContent() string {
	return contextClearedMessagePrefix + "\n\n" + contextClearedSummary
}

func (c *Core) SessionContext(ctx context.Context, sessionID string) (ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return ContextReport{}, err
	}
	return c.contextReportForSession(session, messages), nil
}

func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	before := c.contextReportForSession(session, messages)
	_, effectiveMessages := latestCompactSummary(messages)
	if len(effectiveMessages) == 0 {
		return CompactSessionResult{}, ErrInvalidInput
	}

	summary, err := c.generateCompactSummary(ctx, session, effectiveMessages)
	if err != nil {
		return CompactSessionResult{}, err
	}
	summary = strings.TrimSpace(summary)
	previewContent := compactMessagePrefix + "\n\n" + summary
	preview := Message{
		Role:    MessageRoleSystem,
		Content: previewContent,
		Parts: []MessagePart{{
			Kind: MessagePartKindText,
			Text: &TextPart{Text: previewContent},
		}},
	}
	after := c.contextReportForSession(session, append(append([]Message(nil), messages...), preview))
	content := fmt.Sprintf("%s: ~%s -> ~%s tokens\n\n%s", compactMessagePrefix, FormatShortNumber(before.TokenEstimate), FormatShortNumber(after.TokenEstimate), summary)
	message, err := c.CreateSystemMessage(ctx, session.ID, content)
	if err != nil {
		return CompactSessionResult{}, err
	}

	nextMessages, err := c.store.ListMessages(ctx, session.ID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(session, nextMessages)}, nil
}

func (c *Core) autoCompactSessionIfNeeded(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, report, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	if !report.Compact.Recommended || compactBackoffActive(messages) {
		return false, nil
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages)
}

func (c *Core) forceCompactSessionForRetry(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, _, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages)
}

func (c *Core) providerRequestNeedsCompact(ctx context.Context, turn turnExecution, request providers.Request) bool {
	sessionID := normalizeText(turn.SessionID)
	if sessionID == "" {
		return false
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}
	session = c.decorateSessionLLM(session)
	window := c.sessionContextWindowTokens(session)
	if window <= 0 {
		return false
	}
	threshold := compactThresholdForWindow(window)
	if threshold <= 0 {
		return false
	}
	return EstimateProviderRequestTokens(request) >= threshold
}

func (c *Core) sessionContextSnapshot(ctx context.Context, sessionID string) (Session, []Message, ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return Session{}, nil, ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	return session, messages, c.contextReportForSession(session, messages), nil
}

func (c *Core) compactSessionWithLoadedMessages(ctx context.Context, session Session, messages []Message) (bool, error) {
	_, effectiveMessages := latestCompactSummary(messages)
	if len(effectiveMessages) == 0 {
		return false, nil
	}
	_, err := c.CompactSession(ctx, session.ID)
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}

func (c *Core) contextReportForSession(session Session, messages []Message) ContextReport {
	report := c.contextReport(session.ID, messages)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	report.Compact = compactRecommendationForWindow(report.TokenEstimate, report.WindowTokens)
	return report
}

func (c *Core) contextReport(sessionID string, messages []Message) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := AssistantSystemPrompt(assistant)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	marker := latestContextMarker(messages)
	effectiveMessages := marker.effectiveMessages

	blocks := make([]ContextBlock, 0, 4)
	if systemPrompt != "" {
		blocks = append(blocks, ContextBlock{
			ID:             "system",
			Kind:           ContextBlockSystemPrompt,
			Source:         "assistant_profile",
			TokenEstimate:  EstimateTextTokens(systemPrompt),
			Included:       true,
			CacheStability: "stable",
		})
	}
	if customInstructions != "" {
		blocks = append(blocks, ContextBlock{
			ID:             "custom_instructions",
			Kind:           ContextBlockCustomInstructions,
			Source:         "assistant_profile",
			TokenEstimate:  EstimateTextTokens(customInstructions),
			Included:       true,
			CacheStability: "stable",
		})
	}
	if marker.summary != "" {
		blocks = append(blocks, ContextBlock{
			ID:             marker.blockID,
			Kind:           marker.blockKind,
			Source:         marker.source,
			TokenEstimate:  EstimateTextTokens(marker.summary),
			Included:       true,
			CacheStability: "stable",
		})
	}
	if len(effectiveMessages) > 0 {
		blocks = append(blocks, ContextBlock{
			ID:             "messages",
			Kind:           ContextBlockMessages,
			Source:         "session_history",
			TokenEstimate:  EstimateMessageTokens(effectiveMessages),
			Included:       true,
			CacheStability: "dynamic",
		})
	}
	if estimate := c.estimateToolSchemaTokens(); estimate > 0 {
		blocks = append(blocks, ContextBlock{
			ID:             "tools",
			Kind:           ContextBlockToolSchemas,
			Source:         "tool_registry",
			TokenEstimate:  estimate,
			Included:       true,
			CacheStability: "stable",
		})
	}

	total := 0
	for _, block := range blocks {
		if block.Included {
			total += block.TokenEstimate
		}
	}
	return ContextReport{
		SessionID:         sessionID,
		Estimated:         true,
		TokenEstimate:     total,
		MessageCount:      len(effectiveMessages),
		Blocks:            blocks,
		LastProviderUsage: latestProviderUsage(effectiveMessages),
		Compact:           compactRecommendation(total),
	}
}

func (c *Core) sessionContextWindowTokens(session Session) int {
	session = c.decorateSessionLLM(session)
	providerID := strings.TrimSpace(session.ProviderID)
	modelID := strings.TrimSpace(session.ModelID)
	providerType := ""
	if llms := c.sessionLLMs(); llms != nil && providerID != "" {
		if manual, ok := llms.(SessionLLMContextWindowRegistry); ok {
			if tokens, found := manual.ContextWindowTokens(providerID, modelID); found && tokens > 0 {
				return tokens
			}
		}
		if option, resolved, err := llms.Normalize(providerID, modelID); err == nil {
			providerType = option.Type
			if strings.TrimSpace(resolved) != "" {
				modelID = resolved
			}
		}
	}
	return providers.ResolveContextWindowTokens(providerID, providerType, modelID)
}

func (c *Core) generateCompactSummary(ctx context.Context, session Session, messages []Message) (string, error) {
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return "", err
	}
	content := "Compact this session history:\n\n" + compactHistoryPrompt(messages)
	if planSnapshot := c.compactSessionPlanSnapshot(ctx, session.ID); planSnapshot != "" {
		content = "Current session plan:\n" + planSnapshot + "\n\n" + content
	}

	response, err := runtime.Generate(ctx, providers.Request{
		SessionID:    session.ID,
		SystemPrompt: compactSummarySystemPrompt(),
		Messages: []providers.Message{{
			Role:    "user",
			Content: content,
		}},
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", errors.New("compact summary is empty")
	}
	return text, nil
}

func (c *Core) compactSessionPlanSnapshot(ctx context.Context, sessionID string) string {
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return ""
	}
	return planToolSummary(plan)
}

func compactSummarySystemPrompt() string {
	return strings.TrimSpace(`You compact matrixclaw chat histories into durable working context for future assistant turns.

Write a concise, factual summary. Preserve only information that helps future turns continue correctly:
- Current user goal and task state.
- Decisions already made and constraints the user gave.
- Files, modules, commands, tools, providers, or services that were changed or investigated.
- Important tool results, failures, approvals, pending actions, and unresolved risks.
- Active plan/task status when it matters for continuing work.

Do not include filler, greetings, speculation, duplicated logs, raw tool dumps, secrets, API keys, OAuth tokens, or long code/output blocks.
If a tool call/result pair matters, summarize the action and outcome together.
Reply in English. Use compact bullet points only when they add clarity.`)
}

func compactHistoryPrompt(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	groups := compactMessageGroups(messages)
	tailStart := compactTailGroupStart(groups, maxCompactPromptRunes*3/4)
	var builder strings.Builder
	if tailStart > 0 {
		builder.WriteString("[older history summarized from pruned excerpts]\n\n")
	}
	for i, group := range groups {
		text := compactGroupTextForSummary(group, i >= tailStart)
		if text == "" {
			continue
		}
		builder.WriteString(text)
		builder.WriteString("\n\n")
	}
	return trimRunesFromStart(builder.String(), maxCompactPromptRunes)
}

type compactMessageGroup struct {
	messages []Message
}

func compactMessageGroups(messages []Message) []compactMessageGroup {
	filtered := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == MessageRoleSystem || IsPlanRunPromptMessage(message) {
			continue
		}
		filtered = append(filtered, message)
	}
	groups := make([]compactMessageGroup, 0, len(filtered))
	for i := 0; i < len(filtered); i++ {
		message := filtered[i]
		group := compactMessageGroup{messages: []Message{message}}
		toolCallIDs := messageToolCallIDs(message)
		for len(toolCallIDs) > 0 && i+1 < len(filtered) && messageIsToolResultFor(filtered[i+1], toolCallIDs) {
			i++
			group.messages = append(group.messages, filtered[i])
		}
		groups = append(groups, group)
	}
	return groups
}

func compactTailGroupStart(groups []compactMessageGroup, tailRuneBudget int) int {
	if tailRuneBudget <= 0 {
		return len(groups)
	}
	used := 0
	lastUserIndex := -1
	for i := len(groups) - 1; i >= 0; i-- {
		if compactGroupHasUserMessage(groups[i]) && lastUserIndex < 0 {
			lastUserIndex = i
		}
		text := compactGroupTextForSummary(groups[i], true)
		next := utf8.RuneCountInString(text) + 16
		if used > 0 && used+next > tailRuneBudget && (lastUserIndex < 0 || i < lastUserIndex) {
			return i + 1
		}
		used += next
	}
	return 0
}

func compactGroupHasUserMessage(group compactMessageGroup) bool {
	for _, message := range group.messages {
		if message.Role == MessageRoleUser {
			return true
		}
	}
	return false
}

func compactGroupTextForSummary(group compactMessageGroup, tail bool) string {
	parts := make([]string, 0, len(group.messages))
	for _, message := range group.messages {
		role := strings.TrimSpace(string(message.Role))
		text := compactMessageTextForSummary(message, tail)
		if role == "" || text == "" {
			continue
		}
		parts = append(parts, role+": "+text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func compactMessageTextForSummary(message Message, tail bool) string {
	text := strings.TrimSpace(message.Content)
	if len(message.Parts) > 0 {
		text = compactMessagePartsTextForSummary(message.Parts, tail)
	}
	if tail {
		return trimRunesEnd(text, 8_000)
	}
	return trimRunesEnd(text, 1_500)
}

func messageToolCallIDs(message Message) map[string]struct{} {
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

func messageIsToolResultFor(message Message, ids map[string]struct{}) bool {
	if len(ids) == 0 || message.Role != MessageRoleTool {
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

func compactMessagePartsTextForSummary(parts []MessagePart, tail bool) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != nil:
			values = append(values, trimRunesEnd(part.Text.Text, compactTextPartLimit(tail)))
		case part.Image != nil:
			values = append(values, "image: "+imagePartLabel(*part.Image))
		case part.ToolCall != nil:
			input := trimRunesEnd(part.ToolCall.Input, compactToolPartLimit(tail))
			values = append(values, "tool call: "+part.ToolCall.Name+" "+input)
		case part.ToolResult != nil:
			content := trimRunesEnd(part.ToolResult.Content, compactToolPartLimit(tail))
			values = append(values, "tool result: "+part.ToolResult.Name+" "+content)
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n"))
}

func compactTextPartLimit(tail bool) int {
	if tail {
		return 8_000
	}
	return 1_500
}

func compactToolPartLimit(tail bool) int {
	if tail {
		return 4_000
	}
	return 800
}

func trimRunesEnd(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "\n[truncated]"
}

func trimRunesFromStart(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return "[older history truncated]\n" + string(runes[len(runes)-maxRunes:])
}

type contextMarker struct {
	summary           string
	effectiveMessages []Message
	blockID           string
	blockKind         ContextBlockKind
	source            string
	cleared           bool
}

func latestContextMarker(messages []Message) contextMarker {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != MessageRoleSystem {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if summary, ok := compactMarkerSummary(content); ok {
			return contextMarker{
				summary:           summary,
				effectiveMessages: messages[i+1:],
				blockID:           "compact_summary",
				blockKind:         ContextBlockCompactSummary,
				source:            "session_compact",
			}
		}
		if summary, ok := clearMarkerSummary(content); ok {
			return contextMarker{
				summary:           summary,
				effectiveMessages: messages[i+1:],
				blockID:           "clear_marker",
				blockKind:         ContextBlockClearMarker,
				source:            "session_clear",
				cleared:           true,
			}
		}
	}
	return contextMarker{effectiveMessages: messages}
}

func latestCompactSummary(messages []Message) (string, []Message) {
	marker := latestContextMarker(messages)
	return marker.summary, marker.effectiveMessages
}

func latestCompactSummaryForRun(messages []Message, currentRunID string) (string, []Message) {
	currentRunID = strings.TrimSpace(currentRunID)
	if currentRunID == "" {
		return latestCompactSummary(messages)
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != MessageRoleSystem {
			continue
		}
		content := strings.TrimSpace(message.Content)
		summary, ok := compactMarkerSummary(content)
		cleared := false
		if !ok {
			summary, ok = clearMarkerSummary(content)
			cleared = ok
		}
		if !ok {
			continue
		}
		effective := make([]Message, 0, len(messages)-i-1)
		if !cleared {
			for _, prior := range messages[:i] {
				if strings.TrimSpace(prior.RunID) == currentRunID {
					effective = append(effective, prior)
				}
			}
		}
		effective = append(effective, messages[i+1:]...)
		return summary, effective
	}
	return "", messages
}

func compactMarkerSummary(content string) (string, bool) {
	if !strings.HasPrefix(content, compactMessagePrefix) {
		return "", false
	}
	summary := strings.TrimSpace(strings.TrimPrefix(content, compactMessagePrefix))
	if strings.HasPrefix(summary, ":") {
		if _, tail, ok := strings.Cut(summary, "\n\n"); ok {
			summary = strings.TrimSpace(tail)
		}
	}
	return summary, true
}

func clearMarkerSummary(content string) (string, bool) {
	if !strings.HasPrefix(content, contextClearedMessagePrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(content, contextClearedMessagePrefix)), true
}

var compactStatsPattern = regexp.MustCompile(`~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s*->\s*~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s+tokens`)

func compactBackoffActive(messages []Message) bool {
	checked := 0
	for i := len(messages) - 1; i >= 0 && checked < 2; i-- {
		content := strings.TrimSpace(messages[i].Content)
		if !strings.HasPrefix(content, compactMessagePrefix) {
			continue
		}
		checked++
		before, after, ok := parseCompactStats(content)
		if !ok || before <= 0 {
			return false
		}
		saved := before - after
		if saved*100 >= before*compactBackoffMinimumSavingsPercent {
			return false
		}
	}
	return checked >= 2
}

func parseCompactStats(content string) (int, int, bool) {
	match := compactStatsPattern.FindStringSubmatch(content)
	if len(match) != 3 {
		return 0, 0, false
	}
	before, ok := parseShortTokenNumber(match[1])
	if !ok {
		return 0, 0, false
	}
	after, ok := parseShortTokenNumber(match[2])
	if !ok {
		return 0, 0, false
	}
	return before, after, true
}

func parseShortTokenNumber(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "k"):
		multiplier = 1_000
		value = strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier = 1_000_000
		value = strings.TrimSuffix(value, "m")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return int(parsed * multiplier), true
}

func (c *Core) estimateToolSchemaTokens() int {
	if c.tools == nil {
		return 0
	}
	total := 0
	for _, tool := range c.tools.List() {
		total += EstimateTextTokens(tool.ID)
		total += EstimateTextTokens(tool.Description)
		total += EstimateTextTokens(string(tool.InputJSONSchema))
	}
	return total
}

func EstimateMessageTokens(messages []Message) int {
	total := 0
	for _, message := range messages {
		messageTotal := 0
		for _, part := range message.Parts {
			switch part.Kind {
			case MessagePartKindText:
				if part.Text != nil {
					messageTotal += EstimateTextTokens(part.Text.Text)
				}
			case MessagePartKindImage:
				if part.Image != nil {
					messageTotal += EstimatedImageTokens
				}
			case MessagePartKindToolCall:
				if part.ToolCall != nil {
					messageTotal += EstimateTextTokens(part.ToolCall.Name)
					messageTotal += EstimateTextTokens(part.ToolCall.Input)
				}
			case MessagePartKindToolResult:
				if part.ToolResult != nil {
					messageTotal += EstimateTextTokens(part.ToolResult.Name)
					messageTotal += EstimateTextTokens(part.ToolResult.Content)
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

func EstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return max(1, (utf8.RuneCountInString(text)+3)/4)
}

func compactRecommendation(tokens int) ContextCompact {
	const defaultCompactThreshold = 80_000
	if tokens < defaultCompactThreshold {
		return ContextCompact{}
	}
	return ContextCompact{
		Recommended: true,
		Reason:      "estimated context is high; compact session history before continuing",
	}
}

func compactRecommendationForWindow(tokens int, windowTokens int) ContextCompact {
	if windowTokens <= 0 {
		return compactRecommendation(tokens)
	}
	threshold := compactThresholdForWindow(windowTokens)
	if tokens < threshold {
		return ContextCompact{}
	}
	return ContextCompact{
		Recommended: true,
		Reason:      "estimated context is high for the selected model; compact session history before continuing",
	}
}

func compactThresholdForWindow(windowTokens int) int {
	if windowTokens <= 0 {
		return 0
	}
	threshold := windowTokens / 2
	const minimumThreshold = 80_000
	if threshold < minimumThreshold {
		threshold = minimumThreshold
	}
	return threshold
}

func EstimateProviderRequestTokens(request providers.Request) int {
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

func latestProviderUsage(messages []Message) *ProviderUsage {
	for i := len(messages) - 1; i >= 0; i-- {
		for j := len(messages[i].Parts) - 1; j >= 0; j-- {
			part := messages[i].Parts[j]
			if part.Kind != MessagePartKindFinish || part.Finish == nil || len(part.Finish.Details) == 0 {
				continue
			}
			var payload struct {
				Usage ProviderUsage `json:"usage"`
			}
			if err := json.Unmarshal(part.Finish.Details, &payload); err == nil && !providerUsageEmpty(payload.Usage) {
				usage := payload.Usage
				return &usage
			}
		}
	}
	return nil
}

func providerUsageEmpty(usage ProviderUsage) bool {
	return usage.InputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.TotalTokens == 0 &&
		usage.CachedTokens == 0 &&
		usage.ReasoningTokens == 0 &&
		len(usage.ProviderRaw) == 0
}
