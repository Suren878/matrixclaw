package core

import (
	"context"
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
	if runtimeToolUseAllowed(turn.Runtime) && clientSupportsVoiceDelivery(turn.ClientCapabilities) {
		sections = append(sections, voiceOutputGuidancePrompt())
	}
	if runtimeToolUseAllowed(turn.Runtime) && clientSupportsDocumentDelivery(turn.ClientCapabilities) && c.fileDeliveryPromptAvailable() {
		sections = append(sections, fileDeliveryGuidancePrompt())
	}
	if runtimeToolUseAllowed(turn.Runtime) && c.telephonyCallPromptAvailable() {
		sections = append(sections, telephonyCallGuidancePrompt())
	}
	if runtimeToolUseAllowed(turn.Runtime) {
		sections = append(sections, toolUseDisciplinePrompt())
	}
	if workingDir := strings.TrimSpace(turn.WorkingDir); workingDir != "" {
		sections = append(sections, currentProjectRootPrompt(workingDir))
	}
	if c.webResearchPromptAvailable() {
		sections = append(sections, webResearchGuidancePrompt())
	}
	if statusPrompt := c.runtimeStatusPromptContext(ctx, turn); statusPrompt != "" {
		sections = append(sections, statusPrompt)
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

func (c *Core) runtimeStatusPromptContext(ctx context.Context, turn turnExecution) string {
	if c == nil || c.runtimeStatus == nil {
		return ""
	}
	return c.runtimeStatus.RuntimeStatusPromptContext(ctx, RuntimeStatusContextRequest{
		SessionID:  turn.SessionID,
		RunID:      turn.RunID,
		WorkingDir: turn.WorkingDir,
		ToolIDs:    c.runtimeStatusToolIDs(turn),
	})
}

func (c *Core) runtimeStatusToolIDs(turn turnExecution) []string {
	if c == nil || c.tools == nil || !runtimeToolUseAllowed(turn.Runtime) {
		return nil
	}
	specs := c.tools.List()
	ids := make([]string, 0, len(specs))
	for _, spec := range specs {
		if turn.Subagent && !subagentToolAllowed(spec) {
			continue
		}
		if spec.ID == "text_to_speech" && !clientSupportsVoiceDelivery(turn.ClientCapabilities) {
			continue
		}
		if spec.ID == "send_file" && !clientSupportsDocumentDelivery(turn.ClientCapabilities) {
			continue
		}
		ids = append(ids, spec.ID)
	}
	return ids
}

func (c *Core) providerToolDefinitions(ctx context.Context, turn turnExecution) []providers.ToolDefinition {
	if c.tools != nil {
		specs := c.tools.List()
		definitions := make([]providers.ToolDefinition, 0, len(specs))
		for _, spec := range specs {
			if turn.Subagent && !subagentToolAllowed(spec) {
				continue
			}
			if spec.ID == "text_to_speech" && !clientSupportsVoiceDelivery(turn.ClientCapabilities) {
				continue
			}
			if spec.ID == "send_file" && !clientSupportsDocumentDelivery(turn.ClientCapabilities) {
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

func clientSupportsVoiceDelivery(capabilities ClientCapabilities) bool {
	return capabilities.SupportsVoiceDelivery
}

func clientSupportsDocumentDelivery(capabilities ClientCapabilities) bool {
	return capabilities.SupportsDocumentDelivery
}

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
