package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/skills"
)

func (d *Dispatcher) skillBodyEditor(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return textResult(err.Error()), nil
	}
	return Result{Screen: PromptData{
		Title:         "Edit " + skillTitle(detail.Skill),
		Placeholder:   "Skill instructions",
		Value:         detail.Body,
		Submit:        skillsSaveBody.command(section, skillID),
		CancelCommand: skillsEdit.command(section, skillID),
		Multiline:     true,
	}}, nil
}

func (d *Dispatcher) skillView(ctx context.Context, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return Result{}, err
	}
	body := strings.TrimSpace(detail.Body)
	if len(body) > 4000 {
		body = body[:4000] + "\n..."
	}
	return Result{
		Screen: InfoData{
			Title: skillTitle(detail.Skill),
			Text:  body,
		},
	}, nil
}

func (d *Dispatcher) skillSearchPrompt() Result {
	return Result{Screen: PromptData{
		Title:         "Search Skills",
		Placeholder:   "deploy, review, docs",
		Submit:        skillsSearch.command(),
		CancelCommand: skillsRoot.command(),
	}}
}

func (d *Dispatcher) skillInstallPrompt() Result {
	return Result{Screen: PromptData{
		Title:         "Skill or Plugin Path",
		Placeholder:   "/path/to/skill-or-plugin",
		Submit:        skillsImport.command(),
		CancelCommand: skillsAdd.command(),
	}}
}

func (d *Dispatcher) skillAICreatePrompt() Result {
	return Result{Screen: PromptData{
		Title:         "Describe Skill",
		Placeholder:   "When should this skill be used?",
		Submit:        skillsAICreate.command(),
		CancelCommand: skillsAdd.command(),
	}}
}

func (d *Dispatcher) skillCreateNamePrompt() Result {
	return Result{Screen: PromptData{
		Title:         "Skill Name",
		Placeholder:   "Matrix UI Editing",
		Submit:        skillsCreateName.command(),
		CancelCommand: skillsAdd.command(),
	}}
}

// skillCreateDescriptionPrompt opens a draft in the form store; the next
// prompts fill it in by its id.
func (d *Dispatcher) skillCreateDescriptionPrompt(name string) Result {
	draftID := d.forms.open(skillDraftState{Name: strings.TrimSpace(name)})
	return Result{Screen: PromptData{
		Title:         "Skill Description",
		Placeholder:   "When this skill should be used",
		Submit:        skillsCreateDescription.command(draftID),
		CancelCommand: skillsAdd.command(),
	}}
}

func (d *Dispatcher) skillCreateTagsPrompt(draftID string, description string) Result {
	state, ok := formValue[skillDraftState](d.forms, draftID)
	if !ok {
		return textResult(skillDraftExpired)
	}
	state.Description = strings.TrimSpace(description)
	d.forms.put(draftID, state)
	return Result{Screen: PromptData{
		Title:         "Skill Tags",
		Placeholder:   "ui, matrix, review",
		Submit:        skillsCreateTags.command(draftID),
		CancelCommand: skillsAdd.command(),
	}}
}

func (d *Dispatcher) skillCreateBodyEditor(draftID string, tags string) Result {
	state, ok := formValue[skillDraftState](d.forms, draftID)
	if !ok {
		return textResult(skillDraftExpired)
	}
	state.Tags = parseTags(tags)
	d.forms.put(draftID, state)
	return Result{Screen: PromptData{
		Title:         "Skill Instructions",
		Placeholder:   "Write the reusable instructions for this skill",
		Value:         defaultSkillDraftBody(state),
		Submit:        skillsCreateSave.command(draftID),
		CancelCommand: skillsAdd.command(),
		Multiline:     true,
	}}
}

const skillDraftExpired = "This skill draft has expired. Start again with Add Skill."

func (d *Dispatcher) skillEditPrompt(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return textResult(err.Error()), nil
	}
	value := strings.Join([]string{detail.Skill.Name, detail.Skill.Description, strings.Join(detail.Skill.Tags, ","), detail.Skill.Category}, " | ")
	return Result{Screen: PromptData{
		Title:         "Edit Skill Metadata",
		Placeholder:   "name | description | tag1,tag2 | category",
		Value:         value,
		Submit:        skillsEdit.command(section, skillID),
		CancelCommand: skillsEdit.command(section, skillID),
	}}, nil
}

func (d *Dispatcher) skillInstall(ctx context.Context, path string) (Result, error) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return d.skillInstallPrompt(), nil
	}
	installed, err := d.daemon.InstallSkill(ctx, path)
	if err != nil {
		return textResult(err.Error()), nil
	}
	result, err := d.skillsSectionPicker(ctx, "review", "")
	if err != nil {
		return Result{}, err
	}
	return result.withPickerMeta(fmt.Sprintf("Installed %d skill(s). Review before use.", len(installed))), nil
}

func (d *Dispatcher) skillCreateSave(ctx context.Context, draftID string, body string) (Result, error) {
	state, ok := formValue[skillDraftState](d.forms, draftID)
	if !ok {
		return textResult(skillDraftExpired), nil
	}
	draft, err := d.daemon.CreateSkillDraft(ctx, skills.DraftRequest{Name: state.Name, Description: state.Description, Tags: state.Tags, Body: body})
	if err != nil {
		return textResult(err.Error()), nil
	}
	d.forms.close(draftID)
	return d.skillPicker(ctx, "review", draft.ID)
}

func (d *Dispatcher) skillAICreate(ctx context.Context, description string) (Result, error) {
	sessionID, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return textResult("Open or create a chat session before creating a skill with AI."), nil
	}
	if session != nil && !d.mayUse(*session) {
		return textResult(unattendedRefusal), nil
	}
	content := strings.Join([]string{
		"AI-assisted Matrixclaw skill creation is active.",
		"Discuss the desired reusable workflow with the user first. Clarify when the skill should be used, what instructions belong in SKILL.md, and any tags or supporting files.",
		"Maintain the skill as an editable draft in chat. Show the draft to the user and apply requested changes in chat; do not call tools for draft edits.",
		"Do not create the skill until the user explicitly says to save/create it.",
		"After explicit save confirmation, call skill_manage with action=create, name, description, and content. This will open a skill approval dialog where the user can read the draft and approve or reject the write.",
		"If the user rejects or asks for more changes, continue discussing and revising the draft in chat. The created skill must remain in quarantine/Needs Review until the user reviews and trusts it in Modules > Skills > Review Queue.",
	}, "\n")
	if _, err := d.daemon.CreateSystemMessage(ctx, sessionID, content); err != nil {
		return Result{}, err
	}
	prompt := strings.Join([]string{
		"I want to create a Matrixclaw skill with AI assistance.",
		"User description: " + strings.TrimSpace(description),
		"First discuss the skill with me and ask short clarifying questions.",
		"Then show a draft SKILL.md. I may ask for changes before saving.",
		"Save the skill only when I explicitly say to save or create it. After that, call skill_manage create so an approval opens with the draft. If I reject it, continue revising in chat.",
	}, "\n")
	if _, err := d.daemon.Send(ctx, core.HandleMessageInput{SessionID: sessionID, Text: prompt, WorkingDir: d.workingDir}); err != nil {
		return Result{}, err
	}
	return Result{
		Screen:         TextData{Text: "AI skill creation started."},
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) skillEdit(ctx context.Context, section string, skillID string, value string) (Result, error) {
	name, description, tags, category := parseSkillMetadataInput(value)
	if _, err := d.daemon.UpdateSkillMetadata(ctx, skillID, skills.MetadataUpdate{Name: name, Description: description, Tags: tags, Category: category}); err != nil {
		return textResult(err.Error()), nil
	}
	return d.skillEditMenu(ctx, section, skillID)
}

func (d *Dispatcher) skillRemoveConfirm(section string, skillID string) Result {
	return Result{
		Screen: deleteConfirmData("Remove skill "+strings.TrimSpace(skillID)+"?", skillsRemoveConfirm.command(section, skillID), skillsOpen.command(section, skillID)),
	}
}
