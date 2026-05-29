package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) buildProviderRequest(ctx context.Context, turn turnExecution) (providers.Request, error) {
	history, err := c.store.ListMessages(ctx, turn.SessionID, 0)
	if err != nil {
		return providers.Request{}, err
	}

	assistant := c.assistantProfile()
	compactSummary, effectiveHistory := latestCompactSummaryForRun(history, turn.RunID)
	request := providers.Request{
		RunID:              turn.RunID,
		SessionID:          turn.SessionID,
		SystemPrompt:       c.providerSystemPrompt(ctx, turn, assistant, compactSummary, effectiveHistory),
		CustomInstructions: assistant.CustomInstructions,
	}
	if !runtimeToolUseAllowed(turn.Runtime) {
		request.Messages = buildTextOnlyProviderConversationForRun(effectiveHistory, turn.RunID)
		request.Messages = providers.NormalizeMessages(request.Messages, providers.ToolUseDisabled)
		return request, nil
	}
	request.Messages, err = c.buildProviderConversation(ctx, effectiveHistory, turn.RunID)
	if err != nil {
		return providers.Request{}, err
	}
	request.Tools = c.providerToolDefinitions(ctx, turn)
	return request, nil
}

func (c *Core) providerSystemPrompt(ctx context.Context, turn turnExecution, assistant AssistantProfile, compactSummary string, history []Message) string {
	sections := []string{AssistantSystemPrompt(assistant)}
	if turn.Subagent {
		sections = append(sections, subagentSystemPrompt())
		if workingDir := strings.TrimSpace(turn.WorkingDir); workingDir != "" {
			sections = append(sections, currentProjectRootPrompt(workingDir))
		}
		return joinPromptSections(sections...)
	}
	if runtimeToolUseAllowed(turn.Runtime) && clientSupportsVoiceDelivery(turn.Client) {
		sections = append(sections, voiceOutputGuidancePrompt())
	}
	if workingDir := strings.TrimSpace(turn.WorkingDir); workingDir != "" {
		sections = append(sections, currentProjectRootPrompt(workingDir))
	}
	if c.delegateTaskPromptAvailable() {
		sections = append(sections, c.delegateTaskGuidancePrompt(ctx))
	}
	if memoryPrompt := c.MemoryPromptContext(ctx, turn.WorkingDir); memoryPrompt != "" {
		sections = append(sections, memoryPrompt)
	}
	if compactSummary != "" {
		sections = append(sections, "Session context summary:\n"+compactSummary)
	}
	if planPrompt := c.sessionPlanPrompt(ctx, turn.SessionID); planPrompt != "" {
		sections = append(sections, planPrompt)
	}
	if skillsPrompt := c.skillsPromptContext(ctx, turn, history); skillsPrompt != "" {
		sections = append(sections, skillsPrompt)
	}
	return joinPromptSections(sections...)
}

func (c *Core) skillsPromptContext(ctx context.Context, turn turnExecution, history []Message) string {
	if c == nil || c.skillsContext == nil {
		return ""
	}
	messages := make([]SkillsPromptMessage, 0, len(history))
	for _, message := range history {
		messages = append(messages, SkillsPromptMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}
	return c.skillsContext.SkillsPromptContext(ctx, SkillsPromptContextRequest{
		SessionID:  turn.SessionID,
		RunID:      turn.RunID,
		WorkingDir: turn.WorkingDir,
		Messages:   messages,
	})
}

func joinPromptSections(sections ...string) string {
	values := make([]string, 0, len(sections))
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			values = append(values, section)
		}
	}
	return strings.Join(values, "\n\n")
}

func (c *Core) providerToolDefinitions(ctx context.Context, turn turnExecution) []providers.ToolDefinition {
	if c.tools != nil {
		specs := c.tools.List()
		definitions := make([]providers.ToolDefinition, 0, len(specs))
		for _, spec := range specs {
			if turn.Subagent && !subagentToolAllowed(spec) {
				continue
			}
			if spec.ID == "text_to_speech" && !clientSupportsVoiceDelivery(turn.Client) {
				continue
			}
			description := spec.Description
			if spec.ID == delegateTaskToolName || spec.ID == spawnSubagentToolName {
				description = c.delegateTaskToolDescription(ctx, description)
			}
			definitions = append(definitions, providers.ToolDefinition{
				Name:        spec.ID,
				Description: description,
				InputSchema: spec.InputJSONSchema,
			})
		}
		return definitions
	}
	return nil
}

func clientSupportsVoiceDelivery(client string) bool {
	return strings.EqualFold(strings.TrimSpace(client), "telegram")
}

const maxProviderImageBytes int64 = 8 * 1024 * 1024

func runtimeToolUseMode(runtime providers.Runtime) (providers.ToolUseMode, bool) {
	profiler, ok := runtime.(providers.RuntimeProfiler)
	if !ok {
		return "", false
	}
	profile := providers.NormalizeRuntimeProfile(profiler.RuntimeProfile())
	return profile.ToolUseMode, true
}

func runtimeToolUseAllowed(runtime providers.Runtime) bool {
	if toolUseMode, ok := runtimeToolUseMode(runtime); ok && toolUseMode == providers.ToolUseDisabled {
		return false
	}
	capabilityProvider, ok := runtime.(providers.RuntimeCapabilityProvider)
	if !ok {
		return true
	}
	return capabilityProvider.ModelCapabilities().ToolCalling
}

func AssistantSystemPrompt(profile AssistantProfile) string {
	name := strings.Join(strings.Fields(profile.Name), " ")
	systemPrompt := strings.TrimSpace(profile.SystemPrompt)
	languageGuidance := responseLanguageGuidancePrompt()
	if name == "" {
		return joinPromptSections(systemPrompt, languageGuidance)
	}
	identity := fmt.Sprintf("Assistant identity:\n- Your configured assistant name is %q. Use this exact name when asked who you are. If older/default instructions mention a different assistant name, this configured name takes precedence.", name)
	if systemPrompt == "" {
		return joinPromptSections(identity, languageGuidance)
	}
	return joinPromptSections(identity, systemPrompt, languageGuidance)
}

func responseLanguageGuidancePrompt() string {
	return "Response language:\n- Reply in the same language the user uses for the current request.\n- If the user mixes languages, use the language that best matches the user's latest request.\n- Do not force English or Russian unless the user asks for that language."
}

func voiceOutputGuidancePrompt() string {
	return "Voice output:\n- When the user asks for spoken, audio, voice, or TTS output, call the text_to_speech tool with the text that should be spoken.\n- Do not use shell commands, Piper runtime inspection, or local audio files for client voice output.\n- After a successful text_to_speech tool call, keep any follow-up text minimal unless the user asks for an explanation."
}

func (c *Core) delegateTaskPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	if _, ok := c.tools.Spec(delegateTaskToolName); ok {
		return true
	}
	_, ok := c.tools.Spec(spawnSubagentToolName)
	return ok
}

func (c *Core) delegateTaskGuidancePrompt(ctx context.Context) string {
	lines := []string{
		"Subagents:",
		"- You have delegate_task for blocking child-agent work and spawn_subagent for async background child-agent work.",
		"- Use delegate_task when the child result is needed before your next response.",
		"- Use spawn_subagent only for independent tasks where you can continue without the result; it returns a handle immediately and the result will be delivered back to this parent session later.",
		"- Use list_subagents and read_subagent_result to inspect async subagents without pulling full child transcripts into context.",
		"- Keep at most 4 active async subagents per parent session.",
		"- Give every async subagent a short name, a bounded goal, expected output, and only the minimum context needed.",
		"- Use isolation=shared for read-only/research tasks and isolation=worktree for independent write-heavy tasks. Do not run multiple writer subagents against the same files.",
		"- Do not create agent teams, shared task lists, or direct communication between subagents. Results return to you, the parent agent.",
		"- Do not call delegate_task or spawn_subagent recursively from child subagent sessions.",
		"- Available runtime configuration:",
	}
	runtimes := c.subagentRuntimeInfo(ctx)
	available := 0
	for _, runtime := range runtimes {
		if runtime.Available {
			available++
		}
		lines = append(lines, "- "+runtime.PromptLine())
	}
	if ids := availableSubagentRuntimeIDs(runtimes); len(ids) > 0 {
		lines = append(lines,
			"- Runtime IDs available for delegate_task: "+strings.Join(ids, ", ")+".",
			"- When asked which subagent runtimes are available, answer from that Runtime IDs list and include the native matrixclaw runtime.",
			"- Russian requests like \"какие субагенты доступны\" or \"какие субагенты подключены\" also refer to that delegate_task Runtime IDs list, not only to your base model/provider.",
			"- For the current configuration, if asked which subagents or subagent runtimes are available or connected, answer exactly: "+strings.Join(ids, ", ")+".",
		)
	}
	lines = append(lines,
		"- Do not select unavailable runtimes.",
		"- If the user names an unavailable runtime, say it is unavailable and offer the available alternatives.",
	)
	if available <= 1 {
		lines = append(lines, "- If the user asks for a subagent without naming a runtime, use matrixclaw and do not ask which runtime.")
	} else {
		lines = append(lines, "- If the user asks for a subagent without naming a runtime, ask the user which runtime to use unless the request itself makes the runtime obvious.")
	}
	return strings.Join(lines, "\n")
}

func (c *Core) delegateTaskToolDescription(ctx context.Context, base string) string {
	runtimes := c.subagentRuntimeInfo(ctx)
	if len(runtimes) == 0 {
		return base
	}
	lines := []string{strings.TrimSpace(base), "Runtime choices:"}
	for _, runtime := range runtimes {
		lines = append(lines, "- "+runtime.PromptLine())
	}
	return strings.Join(lines, "\n")
}

type subagentRuntimeInfo struct {
	Runtime   string
	Label     string
	Available bool
	Detail    string
	Models    []string
}

func (r subagentRuntimeInfo) PromptLine() string {
	status := "unavailable"
	if r.Available {
		status = "available"
	}
	details := []string{}
	if label := strings.TrimSpace(r.Label); label != "" && label != r.Runtime {
		details = append(details, label)
	}
	if len(r.Models) > 0 {
		details = append(details, "models: "+strings.Join(r.Models, ", "))
	}
	if detail := strings.TrimSpace(r.Detail); detail != "" {
		details = append(details, detail)
	}
	if len(details) == 0 {
		return fmt.Sprintf("%s: %s", r.Runtime, status)
	}
	return fmt.Sprintf("%s: %s (%s)", r.Runtime, status, strings.Join(details, "; "))
}

func (c *Core) subagentRuntimeInfo(ctx context.Context) []subagentRuntimeInfo {
	out := []subagentRuntimeInfo{
		{
			Runtime:   string(SubagentRuntimeMatrixClaw),
			Label:     "native MatrixClaw child session",
			Available: true,
		},
	}
	if c == nil || c.externalAgents == nil {
		return out
	}
	for _, descriptor := range c.ExternalAgents(ctx) {
		runtime := subagentRuntimeAlias(descriptor)
		if runtime == "" {
			continue
		}
		models := normalizeModelNames(c.externalAgentModelList(ctx, descriptor.ID))
		out = append(out, subagentRuntimeInfo{
			Runtime:   runtime,
			Label:     strings.TrimSpace(descriptor.DisplayName),
			Available: descriptor.Installed && descriptor.Enabled,
			Detail:    subagentRuntimeDetail(descriptor),
			Models:    models,
		})
	}
	return out
}

func subagentRuntimeAlias(descriptor ExternalAgentDescriptor) string {
	if len(descriptor.Aliases) > 0 {
		return strings.TrimSpace(descriptor.Aliases[0])
	}
	return strings.TrimSpace(descriptor.ID)
}

func subagentRuntimeDetail(descriptor ExternalAgentDescriptor) string {
	if detail := strings.TrimSpace(descriptor.Detail); detail != "" {
		return detail
	}
	if !descriptor.Installed {
		return "not installed"
	}
	if !descriptor.Enabled {
		return "disabled"
	}
	return ""
}

func normalizeModelNames(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func availableSubagentRuntimeIDs(runtimes []subagentRuntimeInfo) []string {
	out := make([]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		if !runtime.Available {
			continue
		}
		id := strings.TrimSpace(runtime.Runtime)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}

func currentProjectRootPrompt(workingDir string) string {
	return fmt.Sprintf("Current project root:\n- The filesystem working directory for this session is %q.\n- Resolve relative filesystem tool paths under this directory.\n- Use paths inside this project root unless the user explicitly asks for another location.", workingDir)
}

func (c *Core) sessionPlanPrompt(ctx context.Context, sessionID string) string {
	if c == nil || c.store == nil {
		return ""
	}
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return ""
	}
	lines := []string{
		"Session goal and plan:",
		"- Use plan tools for multi-step work: plan_get, plan_set_goal, plan_add_item, plan_update_item, plan_clear.",
		"- Skip plans for simple one-step requests; for larger tasks, keep top-level items current and mark completed work done before claiming completion.",
		"- Use subtasks only for genuinely large items; finish subtasks before marking the parent done.",
	}
	if strings.TrimSpace(plan.Goal) != "" {
		lines = append(lines, "- Current goal: "+strings.TrimSpace(plan.Goal))
	}
	depths := PlanItemDepths(plan.Items)
	for i, item := range plan.Items {
		indent := strings.Repeat("  ", min(depths[item.ID], 4))
		lines = append(lines, fmt.Sprintf("- %s%d. [%s] %s (id: %s)", indent, i+1, item.Status, item.Text, item.ID))
	}
	return strings.Join(lines, "\n")
}

// buildProviderConversation is a convenience wrapper for callers that only need
// the textual conversation shape (token/length estimation, previews). It passes
// a nil AttachmentReader, so no attachment IO is performed and the returned
// error is always nil — hence context.Background() is sufficient and discarding
// the error is safe here. Request paths that load attachments must use the
// ctx-aware (c *Core).buildProviderConversation instead.
func buildProviderConversation(history []Message) []providers.Message {
	conversation, _ := buildProviderConversationWithAttachments(context.Background(), history, nil)
	return conversation
}

func (c *Core) buildProviderConversation(ctx context.Context, history []Message, currentRunID string) ([]providers.Message, error) {
	return buildProviderConversationWithAttachmentsForRun(ctx, history, c.attachments, currentRunID)
}

func buildProviderConversationWithAttachments(ctx context.Context, history []Message, reader AttachmentReader) ([]providers.Message, error) {
	return buildProviderConversationWithAttachmentsForRun(ctx, history, reader, "")
}

func buildProviderConversationWithAttachmentsForRun(ctx context.Context, history []Message, reader AttachmentReader, currentRunID string) ([]providers.Message, error) {
	entries, err := convertProviderConversationHistory(ctx, history, reader, currentRunID)
	if err != nil {
		return nil, err
	}
	toolResults := collectProviderToolResults(entries)

	conversation := make([]providers.Message, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		providerMessages := entries[i].messages
		if len(providerMessages) == 0 {
			continue
		}

		providerMessage := providerMessages[0]
		if isPairedToolResultMessage(providerMessage) {
			continue
		}

		if !isToolCallOnlyProviderMessage(providerMessage) {
			conversation = append(conversation, providerMessages...)
			continue
		}

		var batched providers.Message
		batched, i = batchAdjacentToolCallMessages(entries, i)
		conversation = append(conversation, batched)
		conversation = appendProviderToolResults(conversation, batched.ToolCalls, toolResults)
	}
	return conversation, nil
}

type providerConversationEntry struct {
	messages []providers.Message
}

func convertProviderConversationHistory(ctx context.Context, history []Message, reader AttachmentReader, currentRunID string) ([]providerConversationEntry, error) {
	entries := make([]providerConversationEntry, 0, len(history))
	for _, message := range history {
		if skipInternalPlanPromptForProvider(message, currentRunID) {
			continue
		}
		providerMessages, err := toProviderMessages(ctx, message, reader)
		if err != nil {
			return nil, err
		}
		entries = append(entries, providerConversationEntry{messages: providerMessages})
	}
	return entries, nil
}

func collectProviderToolResults(entries []providerConversationEntry) map[string]providers.Message {
	toolResults := make(map[string]providers.Message)
	for _, entry := range entries {
		for _, providerMessage := range entry.messages {
			if !isPairedToolResultMessage(providerMessage) {
				continue
			}
			toolCallID := strings.TrimSpace(providerMessage.ToolCallID)
			if _, exists := toolResults[toolCallID]; exists {
				continue
			}
			toolResults[toolCallID] = providerMessage
		}
	}
	return toolResults
}

func isPairedToolResultMessage(message providers.Message) bool {
	return strings.TrimSpace(message.Role) == string(MessageRoleTool) && strings.TrimSpace(message.ToolCallID) != ""
}

func isToolCallOnlyProviderMessage(message providers.Message) bool {
	return len(message.ToolCalls) > 0 && strings.TrimSpace(message.Content) == ""
}

func batchAdjacentToolCallMessages(entries []providerConversationEntry, start int) (providers.Message, int) {
	batched := entries[start].messages[0]
	for i := start + 1; i < len(entries); i++ {
		if len(entries[i].messages) != 1 {
			return batched, i - 1
		}
		next := entries[i].messages[0]
		if !isAdditionalBatchableToolCallMessage(next) {
			return batched, i - 1
		}
		batched.ToolCalls = append(batched.ToolCalls, next.ToolCalls...)
	}
	return batched, len(entries) - 1
}

func isAdditionalBatchableToolCallMessage(message providers.Message) bool {
	return strings.TrimSpace(message.Role) == string(MessageRoleAssistant) && isToolCallOnlyProviderMessage(message)
}

func appendProviderToolResults(conversation []providers.Message, toolCalls []providers.ToolCall, toolResults map[string]providers.Message) []providers.Message {
	for _, toolCall := range toolCalls {
		toolCallID := strings.TrimSpace(toolCall.ID)
		if toolCallID == "" {
			continue
		}
		if toolResult, ok := toolResults[toolCallID]; ok {
			conversation = append(conversation, toolResult)
			delete(toolResults, toolCallID)
			continue
		}
		conversation = append(conversation, syntheticFailedToolResult(toolCallID))
	}
	return conversation
}

func syntheticFailedToolResult(toolCallID string) providers.Message {
	return providers.Message{
		Role:       string(MessageRoleTool),
		ToolCallID: toolCallID,
		Content:    "Tool execution failed before completion.",
	}
}

func buildTextOnlyProviderConversation(history []Message) []providers.Message {
	return buildTextOnlyProviderConversationForRun(history, "")
}

func buildTextOnlyProviderConversationForRun(history []Message, currentRunID string) []providers.Message {
	conversation := make([]providers.Message, 0, len(history))
	for _, message := range history {
		if message.Role == MessageRoleSystem || skipInternalPlanPromptForProvider(message, currentRunID) {
			continue
		}
		role := string(message.Role)
		content := textOnlyProviderContent(message)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if message.Role == MessageRoleTool {
			role = string(MessageRoleUser)
		}
		conversation = append(conversation, providers.Message{
			Role:    role,
			Content: content,
		})
	}
	return conversation
}

func skipInternalPlanPromptForProvider(message Message, currentRunID string) bool {
	if !IsPlanRunPromptMessage(message) {
		return false
	}
	currentRunID = strings.TrimSpace(currentRunID)
	if currentRunID == "" {
		return true
	}
	return strings.TrimSpace(message.RunID) != currentRunID
}

// IsPlanRunPromptMessage reports whether message is an internal plan runner prompt.
// These prompts are stored so clients can audit local actions, but provider context
// already receives the current plan through sessionPlanPrompt.
func IsPlanRunPromptMessage(message Message) bool {
	if message.Role != MessageRoleUser {
		return false
	}
	content := strings.TrimSpace(message.Content)
	return strings.HasPrefix(content, "Execute the current session plan.") ||
		strings.HasPrefix(content, "Execute the next session plan item.") ||
		strings.HasPrefix(content, "The session plan was updated.")
}

func textOnlyProviderContent(message Message) string {
	var values []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}

	hasToolPart := false
	for _, part := range message.Parts {
		if part.ToolCall != nil || part.ToolResult != nil {
			hasToolPart = true
			break
		}
	}
	if !hasToolPart {
		if message.Role == MessageRoleTool {
			if content := strings.TrimSpace(message.Content); content != "" {
				add("Previous tool result:\n" + content)
			}
		} else {
			add(message.Content)
		}
		if strings.TrimSpace(message.Content) == "" {
			for _, part := range message.Parts {
				if part.Text != nil {
					add(part.Text.Text)
				}
				if part.Image != nil {
					add("Attached image: " + imagePartLabel(*part.Image))
				}
			}
		}
		return strings.Join(values, "\n\n")
	}

	if message.Role != MessageRoleTool {
		add(message.Content)
	}
	for _, part := range message.Parts {
		if part.ToolCall != nil {
			add(formatToolCallAsText(*part.ToolCall))
		}
		if part.ToolResult != nil {
			add(formatToolResultAsText(*part.ToolResult, message.Content))
		}
	}
	return strings.Join(values, "\n\n")
}

func formatToolCallAsText(part ToolCallPart) string {
	name := strings.TrimSpace(part.Name)
	if name == "" {
		name = "unknown"
	}
	input := strings.TrimSpace(part.Input)
	if input == "" {
		return "Previous tool call: " + name
	}
	return "Previous tool call: " + name + "\nInput:\n" + input
}

func formatToolResultAsText(part ToolResultPart, fallbackContent string) string {
	name := strings.TrimSpace(part.Name)
	if name == "" {
		name = "unknown"
	}
	content := strings.TrimSpace(part.Content)
	if content == "" {
		content = strings.TrimSpace(fallbackContent)
	}
	if content == "" {
		content = "(empty result)"
	}
	return "Previous tool result from " + name + ":\n" + content
}

func toProviderMessages(ctx context.Context, message Message, reader AttachmentReader) ([]providers.Message, error) {
	if message.Role == MessageRoleSystem {
		return nil, nil
	}
	if len(message.Parts) == 0 {
		if strings.TrimSpace(message.Content) == "" {
			return nil, nil
		}
		return []providers.Message{{
			Role:    string(message.Role),
			Content: message.Content,
		}}, nil
	}

	var toolCalls []providers.ToolCall
	var imageParts []ImagePart
	var images []providers.ImageContent
	reasoningContent := messageReasoningContent(message.Parts)
	for _, part := range message.Parts {
		if part.Image != nil {
			imagePart := *part.Image
			imageParts = append(imageParts, imagePart)
			image, err := providerImageContent(ctx, imagePart, reader)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(image.DataBase64) != "" {
				images = append(images, image)
			}
		}
		if part.ToolCall != nil {
			toolName := strings.TrimSpace(part.ToolCall.Name)
			toolInput := strings.TrimSpace(part.ToolCall.Input)
			toolCalls = append(toolCalls, providers.ToolCall{
				ID:        strings.TrimSpace(part.ToolCall.ID),
				Name:      toolName,
				Arguments: []byte(toolInput),
			})
		}
	}
	if len(toolCalls) > 0 {
		return []providers.Message{{
			Role:             string(message.Role),
			Content:          messageContentWithAttachmentRefs(message.Content, imageParts),
			ReasoningContent: reasoningContent,
			Images:           images,
			ToolCalls:        toolCalls,
		}}, nil
	}

	for _, part := range message.Parts {
		if part.ToolResult == nil {
			continue
		}
		content := part.ToolResult.Content
		if strings.TrimSpace(content) == "" {
			content = message.Content
		}
		if strings.TrimSpace(content) == "" {
			content = "(empty result)"
		}
		return []providers.Message{{
			Role:       string(message.Role),
			Content:    content,
			ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),
		}}, nil
	}

	if strings.TrimSpace(message.Content) == "" && len(images) == 0 {
		return nil, nil
	}
	return []providers.Message{{
		Role:             string(message.Role),
		Content:          messageContentWithAttachmentRefs(message.Content, imageParts),
		ReasoningContent: reasoningContent,
		Images:           images,
	}}, nil
}

func messageReasoningContent(parts []MessagePart) *string {
	var values []string
	for _, part := range parts {
		if part.Reasoning == nil {
			continue
		}
		values = append(values, part.Reasoning.Text)
	}
	if len(values) == 0 {
		return nil
	}
	value := strings.Join(values, "\n")
	return &value
}

func imagePartLabel(part ImagePart) string {
	name := strings.TrimSpace(part.Name)
	mimeType := strings.TrimSpace(part.MIMEType)
	storagePath := strings.TrimSpace(part.StoragePath)
	if storagePath != "" {
		location := "storage_path=" + storagePath
		if part.Temporary {
			location = "temp_path=" + storagePath
		}
		if name != "" && mimeType != "" {
			return name + " (" + mimeType + ", " + location + ")"
		}
		if name != "" {
			return name + " (" + location + ")"
		}
		if mimeType != "" {
			return mimeType + " (" + location + ")"
		}
		return location
	}
	switch {
	case name != "" && mimeType != "":
		return name + " (" + mimeType + ")"
	case name != "":
		return name
	case mimeType != "":
		return mimeType
	default:
		return "image"
	}
}

func providerImageContent(ctx context.Context, part ImagePart, reader AttachmentReader) (providers.ImageContent, error) {
	image := providers.ImageContent{
		MIMEType:    strings.TrimSpace(part.MIMEType),
		DataBase64:  strings.TrimSpace(part.DataBase64),
		Name:        strings.TrimSpace(part.Name),
		StoragePath: strings.TrimSpace(part.StoragePath),
		Temporary:   part.Temporary,
		Size:        part.Size,
	}
	if image.DataBase64 != "" {
		return image, nil
	}
	if image.StoragePath == "" {
		return image, nil
	}
	if reader == nil {
		return image, fmt.Errorf("read image attachment %s: attachment storage is not configured", image.StoragePath)
	}
	data, err := reader.ReadAttachment(ctx, image.StoragePath, image.Temporary, maxProviderImageBytes)
	if err != nil {
		return image, fmt.Errorf("read image attachment %s: %w", image.StoragePath, err)
	}
	if int64(len(data.Data)) > maxProviderImageBytes {
		return image, fmt.Errorf("read image attachment %s: image is too large: %d bytes exceeds %d", image.StoragePath, len(data.Data), maxProviderImageBytes)
	}
	if image.MIMEType == "" {
		image.MIMEType = strings.TrimSpace(data.MIMEType)
	}
	if image.Name == "" {
		image.Name = strings.TrimSpace(data.Name)
	}
	if image.Size == 0 {
		image.Size = data.Size
	}
	image.DataBase64 = base64.StdEncoding.EncodeToString(data.Data)
	return image, nil
}

func messageContentWithAttachmentRefs(content string, images []ImagePart) string {
	content = strings.TrimSpace(content)
	var refs []string
	for _, image := range images {
		path := strings.TrimSpace(image.StoragePath)
		if path == "" {
			continue
		}
		key := "storage_path"
		if image.Temporary {
			key = "temp_path"
		}
		var fields []string
		fields = append(fields, key+"="+quoteAttachmentValue(path))
		if name := strings.TrimSpace(image.Name); name != "" {
			fields = append(fields, "name="+quoteAttachmentValue(name))
		}
		if mimeType := strings.TrimSpace(image.MIMEType); mimeType != "" {
			fields = append(fields, "mime_type="+quoteAttachmentValue(mimeType))
		}
		if image.Size > 0 {
			fields = append(fields, fmt.Sprintf("size=%d", image.Size))
		}
		refs = append(refs, "- kind=image "+strings.Join(fields, " "))
	}
	if len(refs) == 0 {
		return content
	}
	block := "Attached files:\n" + strings.Join(refs, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}

func quoteAttachmentValue(value string) string {
	return fmt.Sprintf("%q", strings.TrimSpace(value))
}
