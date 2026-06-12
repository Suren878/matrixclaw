package controlplane

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

func (d *Dispatcher) handleSessionSkills(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.skills == nil {
		return unsupportedRuntime("skills"), nil
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.sessionSkillsPicker(ctx, sessionID)
	default:
		action, _ := firstCommandStep(rest)
		switch action {
		case "":
			return d.sessionSkillPicker(ctx, sessionID, step)
		case "view":
			return d.skillView(ctx, step, sessionSkillCommand(step))
		case "use":
			if _, err := d.skills.UseSkill(ctx, sessionID, step); err != nil {
				return Result{Handled: true, Text: err.Error()}, nil
			}
			return d.sessionSkillPicker(ctx, sessionID, step)
		case "unload":
			if err := d.skills.UnloadSkill(ctx, sessionID, step); err != nil {
				return Result{Handled: true, Text: err.Error()}, nil
			}
			return d.sessionSkillPicker(ctx, sessionID, step)
		default:
			return d.sessionSkillPicker(ctx, sessionID, step)
		}
	}
}

func (d *Dispatcher) currentSessionID(ctx context.Context, externalKey string) (string, error) {
	if d.sessions == nil {
		return "", nil
	}
	binding, err := d.sessions.CurrentBinding(ctx, externalKey)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(binding.SessionID), nil
}

func (d *Dispatcher) sessionSkillsPicker(ctx context.Context, sessionID string) (Result, error) {
	return d.sessionSkillsPickerWithBack(ctx, sessionID, "Skills", "")
}

func (d *Dispatcher) sessionSkillsPickerWithBack(ctx context.Context, sessionID string, title string, backCommand string) (Result, error) {
	available, err := d.skills.ListSkills(ctx, skills.SearchOptions{Limit: 200})
	if err != nil {
		return Result{}, err
	}
	active, err := d.skills.SessionSkills(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	activeSet := skillIDSet(active)
	picker := NewPickerData(PickerSessionSkills, title).
		Meta(sessionSkillsMeta(active))
	if strings.TrimSpace(backCommand) != "" {
		picker.Back(backCommand)
	}
	for _, item := range active {
		picker.Item(PickerItem{ID: item.ID, Title: skillTitle(item), Info: "In this chat · " + item.Description, Selected: true, Command: sessionSkillCommand(item.ID)})
	}
	for _, item := range available {
		if !skillAvailableInSession(item) {
			continue
		}
		if _, ok := activeSet[item.ID]; ok {
			continue
		}
		picker.Item(PickerItem{ID: item.ID, Title: skillTitle(item), Info: item.Description, Command: sessionSkillCommand(item.ID)})
	}
	if len(picker.data.Items) == 0 {
		picker.Static("empty", "No available skills", "Trust and enable skills in Modules.")
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) sessionSkillPicker(ctx context.Context, sessionID string, skillID string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{Handled: true, Text: "Skill not found: " + strings.TrimSpace(skillID)}, nil
	}
	active, err := d.skills.SessionSkills(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	isActive := hasSkillID(active, detail.Skill.ID)
	picker := NewPickerData(PickerSessionSkill, skillTitle(detail.Skill)).
		Context(detail.Skill.ID).
		Meta(sessionSkillInfo(detail.Skill, isActive)).
		Back(sessionSkillsCommand()).
		Row("view", "Preview", detail.Skill.Description, sessionSkillCommand(detail.Skill.ID, "view"))
	if isActive {
		picker.Action("unload", "Unload from This Chat", "", sessionSkillCommand(detail.Skill.ID, "unload"))
	} else if skillAvailableInSession(detail.Skill) {
		picker.Action("use", "Use in This Chat", "", sessionSkillCommand(detail.Skill.ID, "use"))
	} else {
		picker.Item(PickerItem{ID: "unavailable", Title: "Unavailable", Info: skillInfo(detail.Skill), Disabled: true})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) handleSkills(ctx context.Context, args string) (Result, error) {
	return d.handleSkillsForExternal(ctx, "", args)
}

func (d *Dispatcher) handleSkillsForExternal(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.skills == nil {
		return unsupportedRuntime("skills"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.skillsRootPicker(ctx)
	case "library", "review", "installed", "drafts":
		if rest == "" {
			return d.skillsSectionPicker(ctx, step, "")
		}
		action, actionRest := firstCommandStep(actionRestFrom(rest))
		id := firstField(rest)
		if action == "" {
			return d.skillPicker(ctx, step, id)
		}
		return d.handleLibrarySkillAction(ctx, step, id, action, actionRest)
	case "usage":
		return d.skillsUsagePicker(ctx, externalKey)
	case "add":
		switch strings.TrimSpace(rest) {
		case "manual", "create":
			return d.skillCreateNamePrompt(), nil
		case "ai", "ai-create":
			return d.skillAICreatePrompt(), nil
		case "import", "install":
			return d.skillInstallPrompt(), nil
		}
		return d.skillAddPicker(), nil
	case "ai-create":
		if strings.TrimSpace(rest) == "" {
			return d.skillAICreatePrompt(), nil
		}
		return d.skillAICreate(ctx, externalKey, rest)
	case "search":
		if rest == "" {
			return d.skillSearchPrompt(), nil
		}
		return d.skillsSectionPicker(ctx, "search", rest)
	case "install", "import":
		if rest == "" {
			return d.skillInstallPrompt(), nil
		}
		return d.skillInstall(ctx, rest)
	case "create":
		return d.skillCreateNamePrompt(), nil
	case "create-name":
		return d.skillCreateDescriptionPrompt(rest), nil
	case "create-description":
		return d.skillCreateTagsPrompt(rest)
	case "create-tags":
		return d.skillCreateBodyEditor(rest)
	case "create-save":
		return d.skillCreateSave(ctx, rest)
	default:
		action, actionRest := firstCommandStep(rest)
		if action == "" {
			return d.skillPicker(ctx, "library", step)
		}
		return d.handleLibrarySkillAction(ctx, "library", step, action, actionRest)
	}
}

func actionRestFrom(rest string) string {
	_, actionRest := firstCommandStep(rest)
	return actionRest
}

func (d *Dispatcher) skillsRootPicker(ctx context.Context) (Result, error) {
	items, err := d.skills.ListSkills(ctx, skillsLibrarySearchOptions())
	if err != nil {
		return Result{}, err
	}
	review, _, _ := splitSkillLibrary(items)
	picker := NewPickerData(PickerSkills, "Skills").
		Back(modulesCommand())
	picker.
		Row("library", "Skill Library", skillCountInfo(len(items)), skillsCommand("library")).
		Row("add", "Add Skill", "Manual, AI, or import", skillsCommand("add"))
	if len(review) > 0 {
		picker.Row("review", "Review Queue", skillCountInfo(len(review)), skillsCommand("review"))
	}
	picker.Row("usage", "Usage Status", "This chat", skillsCommand("usage"))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) skillAddPicker() Result {
	picker := NewPickerData(PickerSkillsSection, "Add Skill").
		Context("add").
		Back(skillsCommand()).
		Row("manual", "Manual Create", "Step-by-step editor", skillsCommand("create")).
		Row("ai", "Create with AI", "Discuss in this chat, then review", skillsCommand("ai-create")).
		Row("import", "Import", "Path, GitHub, plugin, or Hermes tree", skillsCommand("import"))
	return Result{Handled: true, Picker: picker.Ptr()}
}

func (d *Dispatcher) skillsUsagePicker(ctx context.Context, externalKey string) (Result, error) {
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	return d.sessionSkillsPickerWithBack(ctx, sessionID, "Usage Status", skillsCommand())
}

func (d *Dispatcher) skillsSectionPicker(ctx context.Context, section string, query string) (Result, error) {
	opts := skillsLibrarySearchOptions()
	var items []skills.Skill
	var err error
	if strings.TrimSpace(query) == "" {
		items, err = d.skills.ListSkills(ctx, opts)
	} else {
		items, err = d.skills.SearchSkills(ctx, query, opts)
	}
	if err != nil {
		return Result{}, err
	}
	items = filterSkillsSection(items, section)
	title := skillsSectionTitle(section)
	picker := NewPickerData(PickerSkillsSection, title).
		Context(section).
		Meta(strings.TrimSpace(query)).
		Back(skillsCommand())
	for _, item := range items {
		itemSection := section
		if section == "search" || section == "usage" {
			itemSection = skillSectionForItem(item)
		}
		picker.Item(PickerItem{
			ID:      item.ID,
			Title:   skillTitle(item),
			Info:    skillInfo(item),
			Search:  skillSearchText(item),
			Command: skillsCommand(itemSection, item.ID),
		})
	}
	if len(items) == 0 {
		picker.Static("empty", "No skills", skillsSectionEmptyInfo(section))
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) skillPicker(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{Handled: true, Text: "Skill not found: " + strings.TrimSpace(skillID)}, nil
	}
	skill := detail.Skill
	back := skillsCommand(section)
	if section == "" {
		back = skillsCommand()
	}
	picker := NewPickerData(PickerSkill, skillTitle(skill)).
		Context(section+":"+skill.ID).
		Meta(skillInfo(skill)).
		Back(back).
		Row("view", "Preview", skill.Description, skillsCommand(section, skill.ID, "view"))
	if skill.TrustState == skills.TrustTrusted {
		picker.Row("enabled", "Enabled", formatEnabled(skill.Enabled), skillsCommand(section, skill.ID, "enabled"))
		picker.Row("quarantine", "Move to Quarantine", "", skillsCommand(section, skill.ID, "quarantine"))
	} else {
		picker.Row("trust-enable", "Trust & Enable", "", skillsCommand(section, skill.ID, "trust-enable"))
		picker.Row("keep", "Keep Quarantined", "", skillsCommand(section, skill.ID, "keep"))
	}
	picker.Row("edit", "Edit", "", skillsCommand(section, skill.ID, "edit"))
	if skill.State == skills.StateArchived {
		picker.Row("restore", "Restore", "", skillsCommand(section, skill.ID, "restore"))
	} else {
		picker.Row("archive", "Archive", "", skillsCommand(section, skill.ID, "archive"))
	}
	picker.Danger("remove", "Remove", "", skillsCommand(section, skill.ID, "remove"))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) skillEditMenu(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	picker := NewPickerData(PickerSkillsSection, "Edit "+skillTitle(detail.Skill)).
		Context("edit").
		Back(skillsCommand(section, skillID)).
		Row("metadata", "Metadata", "Name, description, tags, category", skillsCommand(section, skillID, "edit-metadata")).
		Row("instructions", "Instructions", "SKILL.md body", skillsCommand(section, skillID, "edit-body"))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) skillEnabledPicker(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.skills.GetSkill(ctx, skillID)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerSkill, "Enabled").
			Context(section + ":" + skillID).
			Meta(skillTitle(detail.Skill)).
			Popup().
			Item(PickerItem{ID: "on", Title: "On", Selected: detail.Skill.Enabled, Command: skillsCommand(section, skillID, "set-enabled", "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: !detail.Skill.Enabled, Command: skillsCommand(section, skillID, "set-enabled", "off")}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) skillSetEnabled(ctx context.Context, section string, skillID string, value string) (Result, error) {
	enabled, ok := parseEnabledChoice(value)
	if !ok {
		return d.skillEnabledPicker(ctx, section, skillID)
	}
	if err := d.skills.SetSkillEnabled(ctx, skillID, enabled); err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.skillPicker(ctx, section, skillID)
}

func (d *Dispatcher) handleLibrarySkillAction(ctx context.Context, section string, skillID string, action string, actionRest string) (Result, error) {
	switch action {
	case "view":
		return d.skillView(ctx, skillID, skillsCommand(section, skillID))
	case "enabled":
		return d.skillEnabledPicker(ctx, section, skillID)
	case "set-enabled":
		return d.skillSetEnabled(ctx, section, skillID, actionRest)
	case "trust-enable":
		if err := d.skills.SkillAction(ctx, skillID, "trust"); err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		if err := d.skills.SetSkillEnabled(ctx, skillID, true); err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		return d.skillPicker(ctx, "library", skillID)
	case "enable":
		if err := d.skills.SetSkillEnabled(ctx, skillID, true); err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		return d.skillPicker(ctx, section, skillID)
	case "disable":
		if err := d.skills.SetSkillEnabled(ctx, skillID, false); err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		return d.skillPicker(ctx, section, skillID)
	case "trust", "quarantine", "archive", "restore", "pin", "unpin":
		if err := d.skills.SkillAction(ctx, skillID, action); err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		return d.skillPicker(ctx, section, skillID)
	case "keep":
		return d.skillsSectionPicker(ctx, section, "")
	case "edit":
		if actionRest == "" {
			return d.skillEditMenu(ctx, section, skillID)
		}
		return d.skillEdit(ctx, section, skillID, actionRest)
	case "edit-metadata":
		return d.skillEditPrompt(ctx, section, skillID)
	case "edit-body":
		return d.skillBodyEditor(ctx, section, skillID)
	case "save-body":
		if err := d.skills.UpdateSkillBody(ctx, skillID, actionRest); err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		return d.skillEditMenu(ctx, section, skillID)
	case "remove":
		if actionRest == "confirm" {
			if err := d.skills.SkillAction(ctx, skillID, "remove"); err != nil {
				return Result{Handled: true, Text: err.Error()}, nil
			}
			return d.skillsSectionPicker(ctx, section, "")
		}
		return d.skillRemoveConfirm(section, skillID), nil
	default:
		return d.skillPicker(ctx, section, skillID)
	}
}

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

func skillsLibrarySearchOptions() skills.SearchOptions {
	return skills.SearchOptions{IncludeQuarantined: true, IncludeArchived: true, IncludeDisabled: true, Limit: 200}
}

func splitSkillLibrary(items []skills.Skill) (review []skills.Skill, installed []skills.Skill, drafts []skills.Skill) {
	for _, item := range items {
		if item.TrustState == skills.TrustQuarantine {
			review = append(review, item)
			continue
		}
		if item.Source == "draft" {
			drafts = append(drafts, item)
			continue
		}
		if item.TrustState == skills.TrustTrusted {
			installed = append(installed, item)
		}
	}
	return review, installed, drafts
}

func filterSkillsSection(items []skills.Skill, section string) []skills.Skill {
	review, installed, drafts := splitSkillLibrary(items)
	switch section {
	case "review":
		return review
	case "installed":
		return installed
	case "usage", "search":
		if section == "search" {
			return items
		}
		return items
	case "drafts":
		return drafts
	case "library":
		return items
	default:
		return items
	}
}

func skillSectionForItem(item skills.Skill) string {
	if item.TrustState == skills.TrustQuarantine {
		return "review"
	}
	return "library"
}

func skillsSectionTitle(section string) string {
	switch section {
	case "review":
		return "Review Queue"
	case "library":
		return "Skill Library"
	case "installed":
		return "Installed Skills"
	case "drafts":
		return "Created Skills"
	case "usage":
		return "Usage Status"
	case "search":
		return "Skill Search"
	default:
		return "Skills"
	}
}

func skillsSectionEmptyInfo(section string) string {
	switch section {
	case "review":
		return "Imported skills that need review will appear here."
	case "drafts":
		return "Created skills will appear here."
	case "library":
		return "Add a SKILL.md directory, plugin bundle, or create a skill."
	default:
		return "Install a SKILL.md directory or plugin bundle."
	}
}

func skillsModuleInfo(items []skills.Skill) string {
	if len(items) == 0 {
		return ""
	}
	trusted := 0
	quarantined := 0
	for _, item := range items {
		if item.TrustState == skills.TrustTrusted && item.State == skills.StateActive && item.Enabled {
			trusted++
		}
		if item.TrustState == skills.TrustQuarantine {
			quarantined++
		}
	}
	if quarantined > 0 {
		return fmt.Sprintf("%d enabled · %d review", trusted, quarantined)
	}
	return fmt.Sprintf("%d enabled", trusted)
}

func skillCountInfo(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d", count)
}

func skillTitle(skill skills.Skill) string {
	if strings.TrimSpace(skill.Name) != "" {
		return strings.TrimSpace(skill.Name)
	}
	return strings.TrimSpace(skill.ID)
}

func skillInfo(skill skills.Skill) string {
	enabled := "Disabled"
	if skill.Enabled {
		enabled = "Enabled"
	}
	parts := []string{skillOriginInfo(skill)}
	if skill.TrustState == skills.TrustQuarantine {
		parts = append(parts, skillTrustInfo(skill))
	}
	parts = append(parts, enabled)
	if skill.State == skills.StateArchived {
		parts = append(parts, "Archived")
	} else if skill.State != "" && skill.State != skills.StateActive {
		parts = append(parts, skill.State)
	}
	return strings.Join(nonEmptyStrings(parts...), " · ")
}

func skillSearchText(skill skills.Skill) string {
	parts := []string{
		skill.ID,
		skill.Name,
		skill.Description,
		skill.Category,
		skill.Source,
		skillOriginInfo(skill),
		skillTrustInfo(skill),
	}
	parts = append(parts, skill.Tags...)
	parts = append(parts, skill.Platforms...)
	return strings.Join(nonEmptyStrings(parts...), " ")
}

func sessionSkillsMeta(active []skills.Skill) string {
	if len(active) == 0 {
		return "No active skills in this chat"
	}
	return fmt.Sprintf("%d active in this chat", len(active))
}

func sessionSkillInfo(skill skills.Skill, active bool) string {
	info := skillInfo(skill)
	if active {
		return "In this chat · " + info
	}
	return info
}

func skillOriginInfo(skill skills.Skill) string {
	switch strings.ToLower(strings.TrimSpace(skill.Source)) {
	case "draft":
		return "Created"
	case "":
		return "Installed"
	default:
		return "Imported"
	}
}

func skillTrustInfo(skill skills.Skill) string {
	switch skill.TrustState {
	case skills.TrustTrusted:
		return "Trusted"
	case skills.TrustQuarantine:
		return "Needs Review"
	default:
		return strings.TrimSpace(skill.TrustState)
	}
}

func skillAvailableInSession(skill skills.Skill) bool {
	return skill.TrustState == skills.TrustTrusted && skill.State == skills.StateActive && skill.Enabled
}

func skillIDSet(items []skills.Skill) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		out[item.ID] = struct{}{}
	}
	return out
}

func hasSkillID(items []skills.Skill, id string) bool {
	id = strings.TrimSpace(id)
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func parseSkillMetadataInput(value string) (string, string, []string, string) {
	parts := splitSkillInput(value, 4)
	return partAt(parts, 0), partAt(parts, 1), parseTags(partAt(parts, 2)), partAt(parts, 3)
}

func splitSkillInput(value string, limit int) []string {
	raw := strings.SplitN(value, "|", limit)
	out := make([]string, len(raw))
	for i, part := range raw {
		out[i] = strings.TrimSpace(part)
	}
	return out
}

func partAt(parts []string, index int) string {
	if index < 0 || index >= len(parts) {
		return ""
	}
	return strings.TrimSpace(parts[index])
}

func parseTags(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func splitSkillPickerContext(value string) (string, string) {
	section, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return "installed", strings.TrimSpace(value)
	}
	if strings.TrimSpace(section) == "" {
		section = "installed"
	}
	return strings.TrimSpace(section), strings.TrimSpace(id)
}

type skillDraftState struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

func encodeSkillDraftState(state skillDraftState) string {
	raw, _ := json.Marshal(state)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSkillDraftState(value string) (skillDraftState, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return skillDraftState{}, false
	}
	var state skillDraftState
	if err := json.Unmarshal(raw, &state); err != nil {
		return skillDraftState{}, false
	}
	return state, true
}

func defaultSkillDraftBody(state skillDraftState) string {
	description := strings.TrimSpace(state.Description)
	if description == "" {
		description = "Use this skill when the task matches its description."
	}
	return "Use this skill when: " + description + "\n\nSteps:\n1. Identify whether the current task matches this skill.\n2. Follow the project conventions before making changes.\n3. Verify the result before finishing."
}
