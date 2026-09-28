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

// corePrompts is the Prompts port of one native run; memory is the memory text
// the run's system prompt was built with and toolIDs the tools listed with it,
// which the engine keeps for the whole run.
type corePrompts struct {
	c       *Core
	turn    nativeTurn
	memory  string
	toolIDs []string
}

func (p *corePrompts) System(ctx context.Context, history []transcript.Message) (string, string) {
	assistant := p.c.assistantProfile()
	if !p.turn.Subagent {
		p.memory = p.c.MemoryPromptContext(ctx, p.turn.WorkingDir)
	}
	p.toolIDs = p.c.nativeStatusToolIDs(p.turn)
	return p.c.nativeSystemPrompt(ctx, p.turn, assistant, p.memory, history), assistant.CustomInstructions
}

// Context is what changes during a run: the recovery notice, the todo list,
// runtime status, memory written since the run started and the session plan.
func (p *corePrompts) Context(ctx context.Context) string {
	var sections []string
	if checkpoint, ok, err := p.c.runCheckpoint(ctx, p.turn.RunID); err == nil && ok {
		sections = append(sections, runCheckpointRecoveryPrompt(checkpoint))
	}
	sections = append(sections, p.c.sessionTodoPrompt(ctx, p.turn.SessionID))
	if p.turn.Subagent {
		return prompt.JoinSections(sections...)
	}
	sections = append(sections, p.c.nativeStatusPrompt(ctx, p.turn, p.toolIDs))
	if memory := p.c.MemoryPromptContext(ctx, p.turn.WorkingDir); memory != p.memory {
		sections = append(sections, memoryChangedPrompt(memory))
	}
	sections = append(sections, p.c.sessionPlanPrompt(ctx, p.turn.SessionID))
	return prompt.JoinSections(sections...)
}

func memoryChangedPrompt(memory string) string {
	if memory == "" {
		return "Memory changed during this run: every entry was removed."
	}
	return "Memory changed during this run; it now is:\n" + memory
}

// nativeSystemPrompt is the part of the prompt that stays fixed for a run.
func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, memory string, history []transcript.Message) string {
	sections := []string{prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)}
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
	if c.delegateTaskPromptAvailable() {
		sections = append(sections, c.delegateTaskGuidancePrompt(ctx))
	}
	sections = append(sections, memory)
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

func (c *Core) nativeStatusPrompt(ctx context.Context, turn nativeTurn, toolIDs []string) string {
	if c == nil || c.runtimeStatus == nil {
		return ""
	}
	return c.runtimeStatus.RuntimeStatusPromptContext(ctx, RuntimeStatusContextRequest{SessionID: turn.SessionID, RunID: turn.RunID, WorkingDir: turn.WorkingDir, ToolIDs: toolIDs})
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
