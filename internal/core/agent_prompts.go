package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/transcript"
	"github.com/Suren878/matrixclaw/internal/workspace"
)

// corePrompts is the Prompts port of one native run; memory is the memory text
// the run's system prompt was built with, toolIDs the tools listed with it and
// runContext the state the run started in, which the engine keeps for the
// whole run.
type corePrompts struct {
	c          *Core
	turn       nativeTurn
	memory     string
	toolIDs    []string
	runContext string
}

func (p *corePrompts) System(ctx context.Context, history []transcript.Message) (string, string) {
	assistant := p.c.assistantProfile()
	if !p.turn.Subagent {
		p.memory = p.c.MemoryPromptContext(ctx, p.turn.WorkingDir)
	}
	p.toolIDs = p.c.nativeStatusToolIDs(p.turn)
	facts := p.c.readRunFacts(ctx, p.turn)
	p.runContext = facts.context.Text()
	return p.c.nativeSystemPrompt(ctx, p.turn, assistant, prompt.NewTools(p.toolIDs...), facts, p.memory, history), assistant.CustomInstructions
}

// Context is what changes between and during runs: the state the run started
// in (clock, client, permission mode, git), the recovery notice, the todo
// list, the background tasks still running, the active session goal, runtime
// status and memory written since the run started.
func (p *corePrompts) Context(ctx context.Context) string {
	sections := []string{p.runContext}
	if checkpoint, ok, err := p.c.runCheckpoint(ctx, p.turn.RunID); err == nil && ok && checkpoint.RecoveryCount > 0 {
		sections = append(sections, runRecoveryNotice)
	}
	sections = append(sections, p.c.sessionTodoPrompt(ctx, p.turn.SessionID, append([]string{p.turn.RunID}, p.turn.Continues...)), p.c.runningTasksPrompt(ctx, p.turn.SessionID))
	if p.turn.Subagent {
		return prompt.JoinSections(sections...)
	}
	sections = append(sections, p.c.goalPrompt(ctx, p.turn), p.c.nativeStatusPrompt(ctx, p.turn, p.toolIDs))
	if memory := p.c.MemoryPromptContext(ctx, p.turn.WorkingDir); memory != p.memory {
		sections = append(sections, memoryChangedPrompt(memory))
	}
	return prompt.JoinSections(sections...)
}

// modelPrompt tells the model what it runs on; a session can switch models, so
// earlier replies in the history may come from another one. A model switch
// changes the system prompt, which no cached prefix survives anyway.
func modelPrompt(model string) string {
	if model == "" {
		return ""
	}
	return "You are running on " + model + ". Earlier replies in this conversation may have come from other models; when asked which model you are, name this one."
}

func memoryChangedPrompt(memory string) string {
	if memory == "" {
		return "Memory changed during this run: every entry was removed."
	}
	return "Memory changed during this run; it now is:\n" + memory
}

// nativeSystemPrompt is the part of the prompt that stays fixed for a run.
// Sections that change least come first, so a changed prompt keeps the
// longest cacheable prefix.
func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, tools prompt.Tools, facts runFacts, memory string, history []transcript.Message) string {
	var sections []string
	if turn.Subagent {
		sections = append(sections, prompt.SubagentPersona(facts.coding, turn.Readonly))
	} else {
		sections = append(sections, prompt.Persona(assistant.Name, assistant.SystemPrompt, facts.coding))
	}
	switch {
	case facts.coding:
		sections = append(sections, prompt.CodingGuidance(tools, turn.Subagent))
	case turn.ToolUse:
		sections = append(sections, prompt.ToolUseDiscipline(tools, turn.Subagent))
	}
	if turn.Subagent {
		sections = append(sections, prompt.SubagentMode(tools, turn.Readonly))
	} else {
		sections = append(sections, prompt.LanguageGuidance())
	}
	if facts.coding {
		sections = append(sections, prompt.Environment(turn.WorkingDir, facts.gitRoot, workspace.Platform()), prompt.ProjectInstructions(facts.instructions))
	}
	if tools.Has("web_fetch") {
		sections = append(sections, prompt.WebGuidance(tools))
	}
	if tools.Has(agentToolName) {
		sections = append(sections, c.subagentsPrompt(ctx, facts.coding))
	}
	if tools.Has("text_to_speech") {
		sections = append(sections, prompt.VoiceOutputGuidance())
	}
	if tools.Has("send_file") {
		sections = append(sections, prompt.FileDeliveryGuidance())
	}
	if tools.Has("telephony_call") {
		sections = append(sections, prompt.TelephonyCallGuidance())
	}
	sections = append(sections, modelPrompt(turn.Model), memory)
	if !turn.Subagent {
		sections = append(sections, c.nativeSkillsPrompt(ctx, turn, history))
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
