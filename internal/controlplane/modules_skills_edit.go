package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

func (d *Dispatcher) skillBodyEditor(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return Result{Handled: true, TextEdit: &TextEditData{
		Title:               "Edit " + skillTitle(detail.Skill),
		Placeholder:         "Skill instructions",
		Value:               detail.Body,
		SubmitCommandPrefix: skillsCommandPrefix(section, skillID, "save-body"),
		CancelCommand:       skillsCommand(section, skillID, "edit"),
	}}, nil
}

func (d *Dispatcher) skillView(ctx context.Context, skillID string, _ string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{}, err
	}
	body := strings.TrimSpace(detail.Body)
	if len(body) > 4000 {
		body = body[:4000] + "\n..."
	}
	return Result{
		Handled: true,
		Info: &InfoData{
			Title: skillTitle(detail.Skill),
			Text:  body,
		},
	}, nil
}

func (d *Dispatcher) skillSearchPrompt() Result {
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Search Skills",
		Placeholder:         "deploy, review, docs",
		SubmitCommandPrefix: skillSearchCommandPrefix(),
		CancelCommand:       skillsCommand(),
	}}
}

func (d *Dispatcher) skillInstallPrompt() Result {
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Skill or Plugin Path",
		Placeholder:         "/path/to/skill-or-plugin",
		SubmitCommandPrefix: skillInstallCommandPrefix(),
		CancelCommand:       skillsCommand("add"),
	}}
}

func (d *Dispatcher) skillAICreatePrompt() Result {
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Describe Skill",
		Placeholder:         "When should this skill be used?",
		SubmitCommandPrefix: skillsCommandPrefix("ai-create"),
		CancelCommand:       skillsCommand("add"),
	}}
}

func (d *Dispatcher) skillCreateNamePrompt() Result {
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Skill Name",
		Placeholder:         "Matrix UI Editing",
		SubmitCommandPrefix: skillsCommandPrefix("create-name"),
		CancelCommand:       skillsCommand("add"),
	}}
}

func (d *Dispatcher) skillCreateDescriptionPrompt(name string) Result {
	state := skillDraftState{Name: strings.TrimSpace(name)}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Skill Description",
		Placeholder:         "When this skill should be used",
		SubmitCommandPrefix: skillsCommandPrefix("create-description", encodeSkillDraftState(state)),
		CancelCommand:       skillsCommand("add"),
	}}
}

func (d *Dispatcher) skillCreateTagsPrompt(rest string) (Result, error) {
	token, description := firstCommandToken(rest)
	state, ok := decodeSkillDraftState(token)
	if !ok {
		return Result{Handled: true, Text: "Invalid skill draft state."}, nil
	}
	state.Description = strings.TrimSpace(description)
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Skill Tags",
		Placeholder:         "ui, matrix, review",
		SubmitCommandPrefix: skillsCommandPrefix("create-tags", encodeSkillDraftState(state)),
		CancelCommand:       skillsCommand("add"),
	}}, nil
}

func (d *Dispatcher) skillCreateBodyEditor(rest string) (Result, error) {
	token, tags := firstCommandToken(rest)
	state, ok := decodeSkillDraftState(token)
	if !ok {
		return Result{Handled: true, Text: "Invalid skill draft state."}, nil
	}
	state.Tags = parseTags(tags)
	return Result{Handled: true, TextEdit: &TextEditData{
		Title:               "Skill Instructions",
		Placeholder:         "Write the reusable instructions for this skill",
		Value:               defaultSkillDraftBody(state),
		SubmitCommandPrefix: skillsCommandPrefix("create-save", encodeSkillDraftState(state)),
		CancelCommand:       skillsCommand("add"),
	}}, nil
}

func (d *Dispatcher) skillEditPrompt(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	value := strings.Join([]string{detail.Skill.Name, detail.Skill.Description, strings.Join(detail.Skill.Tags, ","), detail.Skill.Category}, " | ")
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "Edit Skill Metadata",
		Placeholder:         "name | description | tag1,tag2 | category",
		Value:               value,
		SubmitCommandPrefix: skillsCommandPrefix(section, skillID, "edit"),
		CancelCommand:       skillsCommand(section, skillID, "edit"),
	}}, nil
}

func (d *Dispatcher) skillInstall(ctx context.Context, path string) (Result, error) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return d.skillInstallPrompt(), nil
	}
	installed, err := d.skills.InstallSkill(ctx, path)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	result, err := d.skillsSectionPicker(ctx, "review", "")
	if err != nil {
		return Result{}, err
	}
	if result.Picker != nil {
		result.Picker.Meta = fmt.Sprintf("Installed %d skill(s). Review before use.", len(installed))
	}
	return result, nil
}

func (d *Dispatcher) skillCreateSave(ctx context.Context, rest string) (Result, error) {
	token, body := firstCommandToken(rest)
	state, ok := decodeSkillDraftState(token)
	if !ok {
		return Result{Handled: true, Text: "Invalid skill draft state."}, nil
	}
	draft, err := d.skills.CreateSkillDraft(ctx, state.Name, state.Description, state.Tags, body)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.skillPicker(ctx, "review", draft.ID)
}

func (d *Dispatcher) skillAICreate(ctx context.Context, externalKey string, description string) (Result, error) {
	if d.messages == nil || d.sender == nil {
		return Result{Handled: true, Text: "AI skill creation needs an active chat session."}, nil
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return Result{Handled: true, Text: "Open or create a chat session before creating a skill with AI."}, nil
	}
	content := strings.Join([]string{
		"AI-assisted Matrixclaw skill creation is active.",
		"Discuss the desired reusable workflow with the user first. Clarify when the skill should be used, what instructions belong in SKILL.md, and any tags or supporting files.",
		"Maintain the skill as an editable draft in chat. Show the draft to the user and apply requested changes in chat; do not call tools for draft edits.",
		"Do not create the skill until the user explicitly says to save/create it.",
		"After explicit save confirmation, call skill_manage with action=create, name, description, and content. This will open a skill approval dialog where the user can read the draft and approve or reject the write.",
		"If the user rejects or asks for more changes, continue discussing and revising the draft in chat. The created skill must remain in quarantine/Needs Review until the user reviews and trusts it in Modules > Skills > Review Queue.",
	}, "\n")
	if _, err := d.messages.CreateSystemMessage(ctx, sessionID, content); err != nil {
		return Result{}, err
	}
	prompt := strings.Join([]string{
		"I want to create a Matrixclaw skill with AI assistance.",
		"User description: " + strings.TrimSpace(description),
		"First discuss the skill with me and ask short clarifying questions.",
		"Then show a draft SKILL.md. I may ask for changes before saving.",
		"Save the skill only when I explicitly say to save or create it. After that, call skill_manage create so an approval opens with the draft. If I reject it, continue revising in chat.",
	}, "\n")
	if _, err := d.sender.SendMessage(ctx, sessionID, prompt); err != nil {
		return Result{}, err
	}
	return Result{
		Handled:        true,
		Text:           "AI skill creation started.",
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) skillEdit(ctx context.Context, section string, skillID string, value string) (Result, error) {
	name, description, tags, category := parseSkillMetadataInput(value)
	if _, err := d.skills.UpdateSkillMetadata(ctx, skillID, skills.MetadataUpdate{Name: name, Description: description, Tags: tags, Category: category}); err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.skillEditMenu(ctx, section, skillID)
}

func (d *Dispatcher) skillRemoveConfirm(section string, skillID string) Result {
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Remove skill "+strings.TrimSpace(skillID)+"?", skillsCommand(section, skillID, "remove", "confirm"), skillsCommand(section, skillID)),
	}
}
