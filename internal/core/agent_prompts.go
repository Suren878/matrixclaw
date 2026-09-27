package core

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) webResearchPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("web_research")
	return ok
}

func (c *Core) fileDeliveryPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("send_file")
	return ok
}

func (c *Core) telephonyCallPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("telephony_call")
	return ok
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

// corePrompts is the Prompts port of one native run.
type corePrompts struct {
	c    *Core
	turn nativeTurn
}

func (p corePrompts) System(ctx context.Context, history []transcript.Message) (string, string) {
	assistant := p.c.assistantProfile()
	return p.c.nativeSystemPrompt(ctx, p.turn, assistant, history), assistant.CustomInstructions
}

func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, history []transcript.Message) string {
	sections := []string{prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)}
	if checkpoint, ok, err := c.runCheckpoint(ctx, turn.RunID); err == nil && ok {
		if recoveryPrompt := runCheckpointRecoveryPrompt(checkpoint); recoveryPrompt != "" {
			sections = append(sections, recoveryPrompt)
		}
	}
	workingDir := strings.TrimSpace(turn.WorkingDir)
	if turn.Subagent {
		sections = append(sections, subagentSystemPrompt())
		if workingDir != "" {
			sections = append(sections, prompt.ProjectRoot(workingDir))
		}
		return prompt.JoinSections(sections...)
	}
	if turn.ToolUse && clientSupportsVoiceDelivery(turn.ClientCapabilities) {
		sections = append(sections, prompt.VoiceOutputGuidance())
	}
	if turn.ToolUse && clientSupportsDocumentDelivery(turn.ClientCapabilities) && c.fileDeliveryPromptAvailable() {
		sections = append(sections, prompt.FileDeliveryGuidance())
	}
	if turn.ToolUse && c.telephonyCallPromptAvailable() {
		sections = append(sections, prompt.TelephonyCallGuidance())
	}
	if turn.ToolUse {
		sections = append(sections, prompt.ToolUseDiscipline())
	}
	if workingDir != "" {
		sections = append(sections, prompt.ProjectRoot(workingDir))
	}
	if c.webResearchPromptAvailable() {
		sections = append(sections, prompt.WebResearchGuidance())
	}
	if statusPrompt := c.nativeStatusPrompt(ctx, turn); statusPrompt != "" {
		sections = append(sections, statusPrompt)
	}
	if c.delegateTaskPromptAvailable() {
		sections = append(sections, c.delegateTaskGuidancePrompt(ctx))
	}
	if memoryPrompt := c.MemoryPromptContext(ctx, turn.WorkingDir); memoryPrompt != "" {
		sections = append(sections, memoryPrompt)
	}
	if planPrompt := c.sessionPlanPrompt(ctx, turn.SessionID); planPrompt != "" {
		sections = append(sections, planPrompt)
	}
	if skillsPrompt := c.nativeSkillsPrompt(ctx, turn, history); skillsPrompt != "" {
		sections = append(sections, skillsPrompt)
	}
	return prompt.JoinSections(sections...)
}

func (c *Core) nativeSkillsPrompt(ctx context.Context, turn nativeTurn, history []transcript.Message) string {
	if c == nil || c.skillsContext == nil {
		return ""
	}
	messages := make([]SkillsPromptMessage, 0, len(history))
	for _, message := range history {
		messages = append(messages, SkillsPromptMessage{Role: string(message.Role), Content: message.Content})
	}
	return c.skillsContext.SkillsPromptContext(ctx, SkillsPromptContextRequest{SessionID: turn.SessionID, RunID: turn.RunID, WorkingDir: turn.WorkingDir, Messages: messages})
}

func (c *Core) nativeStatusPrompt(ctx context.Context, turn nativeTurn) string {
	if c == nil || c.runtimeStatus == nil {
		return ""
	}
	return c.runtimeStatus.RuntimeStatusPromptContext(ctx, RuntimeStatusContextRequest{SessionID: turn.SessionID, RunID: turn.RunID, WorkingDir: turn.WorkingDir, ToolIDs: c.nativeStatusToolIDs(turn)})
}

func (c *Core) nativeStatusToolIDs(turn nativeTurn) []string {
	if c == nil || c.tools == nil || !turn.ToolUse {
		return nil
	}
	specs := c.nativeToolSpecs(turn)
	ids := make([]string, 0, len(specs))
	for _, spec := range specs {
		ids = append(ids, spec.ID)
	}
	return ids
}
