package controlplane

import (
	"context"
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
			Select(skillsCommand(section, skillID)).
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
