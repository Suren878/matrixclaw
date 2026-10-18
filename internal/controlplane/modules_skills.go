package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

var (
	sessionSkillsRoot  = valueVerb(CommandSkills, "", 0)
	sessionSkillOpen   = verb(CommandSkills, "open", 1)
	sessionSkillView   = verb(CommandSkills, "view", 1)
	sessionSkillUse    = verb(CommandSkills, "use", 1)
	sessionSkillUnload = verb(CommandSkills, "unload", 1)

	skillsRoot              = verb(CommandModules, "skills", 0)
	skillsSection           = verb(CommandModules, "skills section", 1)
	skillsUsage             = verb(CommandModules, "skills usage", 0)
	skillsAdd               = verb(CommandModules, "skills add", 0)
	skillsSearch            = valueVerb(CommandModules, "skills search", 0)
	skillsImport            = valueVerb(CommandModules, "skills import", 0)
	skillsAICreate          = valueVerb(CommandModules, "skills ai-create", 0)
	skillsCreate            = verb(CommandModules, "skills create", 0)
	skillsCreateName        = valueVerb(CommandModules, "skills create-name", 0)
	skillsCreateDescription = valueVerb(CommandModules, "skills create-description", 1)
	skillsCreateTags        = valueVerb(CommandModules, "skills create-tags", 1)
	skillsCreateSave        = valueVerb(CommandModules, "skills create-save", 1)
	skillsOpen              = verb(CommandModules, "skills open", 2)
	skillsAction            = verb(CommandModules, "skills action", 3)
	skillsSetEnabled        = verb(CommandModules, "skills set-enabled", 3)
	skillsEdit              = valueVerb(CommandModules, "skills edit", 2)
	skillsSaveBody          = valueVerb(CommandModules, "skills save-body", 2)
	skillsRemoveConfirm     = verb(CommandModules, "skills remove-confirm", 2)
)

func (d *Dispatcher) handleSessionSkills(ctx context.Context, r Route) (Result, error) {
	sessionID, err := d.currentSessionID(ctx)
	if err != nil {
		return Result{}, err
	}
	skillID := r.arg(0)
	if r.Is(sessionSkillsRoot) {
		if r.Value == "" {
			return d.sessionSkillsPicker(ctx, sessionID)
		}
		skillID = r.Value
	}
	switch r.Verb {
	case sessionSkillView:
		return d.skillView(ctx, skillID)
	case sessionSkillUse:
		if _, err := d.daemon.UseSkill(ctx, sessionID, skillID); err != nil {
			return textResult(err.Error()), nil
		}
	case sessionSkillUnload:
		if err := d.daemon.UnloadSkill(ctx, sessionID, skillID); err != nil {
			return textResult(err.Error()), nil
		}
	}
	return d.sessionSkillPicker(ctx, sessionID, skillID)
}

func (d *Dispatcher) currentSessionID(ctx context.Context) (string, error) {
	binding, err := d.currentBinding(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(binding.SessionID), nil
}

func (d *Dispatcher) sessionSkillsPicker(ctx context.Context, sessionID string) (Result, error) {
	return d.sessionSkillsPickerWithBack(ctx, sessionID, "Skills", sessionSkillsRoot.command(), "")
}

func (d *Dispatcher) sessionSkillsPickerWithBack(ctx context.Context, sessionID string, title string, command string, backCommand string) (Result, error) {
	available, err := d.daemon.ListSkills(ctx, skills.SearchOptions{Limit: 200})
	if err != nil {
		return Result{}, err
	}
	active, err := d.daemon.SessionSkills(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	activeSet := skillIDSet(active)
	picker := NewPickerData(PickerSessionSkills, title).
		Command(command).
		Meta(sessionSkillsMeta(active))
	if strings.TrimSpace(backCommand) != "" {
		picker.Back(backCommand)
	}
	for _, item := range active {
		picker.Item(PickerItem{ID: item.ID, Title: skillTitle(item), Info: "In this chat · " + item.Description, Selected: true, Command: sessionSkillOpen.command(item.ID)})
	}
	for _, item := range available {
		if !skillAvailableInSession(item) {
			continue
		}
		if _, ok := activeSet[item.ID]; ok {
			continue
		}
		picker.Item(PickerItem{ID: item.ID, Title: skillTitle(item), Info: item.Description, Command: sessionSkillOpen.command(item.ID)})
	}
	if len(picker.data.Items) == 0 {
		picker.Static("empty", "No available skills", "Trust and enable skills in Modules.")
	}
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) sessionSkillPicker(ctx context.Context, sessionID string, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return textResult("Skill not found: " + strings.TrimSpace(skillID)), nil
	}
	active, err := d.daemon.SessionSkills(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	isActive := hasSkillID(active, detail.Skill.ID)
	picker := NewPickerData(PickerSessionSkill, skillTitle(detail.Skill)).
		Meta(sessionSkillInfo(detail.Skill, isActive)).
		Back(sessionSkillsRoot.command()).
		Row("view", "Preview", detail.Skill.Description, sessionSkillView.command(detail.Skill.ID))
	if isActive {
		picker.Action("unload", "Unload from This Chat", "", sessionSkillUnload.command(detail.Skill.ID))
	} else if skillAvailableInSession(detail.Skill) {
		picker.Action("use", "Use in This Chat", "", sessionSkillUse.command(detail.Skill.ID))
	} else {
		picker.Item(PickerItem{ID: "unavailable", Title: "Unavailable", Info: skillInfo(detail.Skill), Disabled: true})
	}
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) handleSkillsForExternal(ctx context.Context, r Route) (Result, error) {
	section, skillID := r.arg(0), r.arg(1)
	switch r.Verb {
	case skillsSection:
		return d.skillsSectionPicker(ctx, section, "")
	case skillsUsage:
		return d.skillsUsagePicker(ctx)
	case skillsAdd:
		return d.skillAddPicker(), nil
	case skillsAICreate:
		if r.Value == "" {
			return d.skillAICreatePrompt(), nil
		}
		return d.skillAICreate(ctx, r.Value)
	case skillsSearch:
		if r.Value == "" {
			return d.skillSearchPrompt(), nil
		}
		return d.skillsSectionPicker(ctx, "search", r.Value)
	case skillsImport:
		return d.skillInstall(ctx, r.Value)
	case skillsCreate:
		return d.skillCreateNamePrompt(), nil
	case skillsCreateName:
		return d.skillCreateDescriptionPrompt(r.Value), nil
	case skillsCreateDescription:
		return d.skillCreateTagsPrompt(r.arg(0), r.Value), nil
	case skillsCreateTags:
		return d.skillCreateBodyEditor(r.arg(0), r.Value), nil
	case skillsCreateSave:
		return d.skillCreateSave(ctx, r.arg(0), r.Value)
	case skillsOpen:
		return d.skillPicker(ctx, section, skillID)
	case skillsAction:
		return d.handleLibrarySkillAction(ctx, section, skillID, r.arg(2))
	case skillsSetEnabled:
		return d.skillSetEnabled(ctx, section, skillID, r.arg(2))
	case skillsEdit:
		if r.Value == "" {
			return d.skillEditMenu(ctx, section, skillID)
		}
		return d.skillEdit(ctx, section, skillID, r.Value)
	case skillsSaveBody:
		if err := d.daemon.UpdateSkillBody(ctx, skillID, r.Value); err != nil {
			return textResult(err.Error()), nil
		}
		return d.skillEditMenu(ctx, section, skillID)
	case skillsRemoveConfirm:
		if err := d.daemon.RemoveSkill(ctx, skillID); err != nil {
			return textResult(err.Error()), nil
		}
		return d.skillsSectionPicker(ctx, section, "")
	default:
		return d.skillsRootPicker(ctx)
	}
}

func (d *Dispatcher) skillsRootPicker(ctx context.Context) (Result, error) {
	items, err := d.daemon.ListSkills(ctx, skillsLibrarySearchOptions())
	if err != nil {
		return Result{}, err
	}
	review, _, _ := splitSkillLibrary(items)
	picker := NewPickerData(PickerSkills, "Skills").
		Back(modulesRoot.command())
	picker.
		Row("library", "Skill Library", skillCountInfo(len(items)), skillsSection.command("library")).
		Row("add", "Add Skill", "Manual, AI, or import", skillsAdd.command())
	if len(review) > 0 {
		picker.Row("review", "Review Queue", skillCountInfo(len(review)), skillsSection.command("review"))
	}
	picker.Row("usage", "Usage Status", "This chat", skillsUsage.command())
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) skillAddPicker() Result {
	picker := NewPickerData(PickerSkillsSection, "Add Skill").
		Back(skillsRoot.command()).
		Row("manual", "Manual Create", "Step-by-step editor", skillsCreate.command()).
		Row("ai", "Create with AI", "Discuss in this chat, then review", skillsAICreate.command()).
		Row("import", "Import", "Path, GitHub, plugin, or Hermes tree", skillsImport.command())
	return Result{Screen: picker.Build()}
}

func (d *Dispatcher) skillsUsagePicker(ctx context.Context) (Result, error) {
	sessionID, err := d.currentSessionID(ctx)
	if err != nil {
		return Result{}, err
	}
	return d.sessionSkillsPickerWithBack(ctx, sessionID, "Usage Status", skillsUsage.command(), skillsRoot.command())
}

func (d *Dispatcher) skillsSectionPicker(ctx context.Context, section string, query string) (Result, error) {
	opts := skillsLibrarySearchOptions()
	var items []skills.Skill
	var err error
	if strings.TrimSpace(query) == "" {
		items, err = d.daemon.ListSkills(ctx, opts)
	} else {
		items, err = d.daemon.SearchSkills(ctx, query, opts)
	}
	if err != nil {
		return Result{}, err
	}
	items = filterSkillsSection(items, section)
	title := skillsSectionTitle(section)
	picker := NewPickerData(PickerSkillsSection, title).
		Command(skillsSectionCommand(section, query)).
		Meta(strings.TrimSpace(query)).
		Back(skillsRoot.command())
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
			Command: skillsOpen.command(itemSection, item.ID),
		})
	}
	if len(items) == 0 {
		picker.Static("empty", "No skills", skillsSectionEmptyInfo(section))
	}
	return Result{Screen: picker.Build()}, nil
}

func skillsSectionCommand(section string, query string) string {
	if section == "search" {
		return skillsSearch.commandValue(query)
	}
	return skillsSection.command(section)
}

func (d *Dispatcher) skillPicker(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return textResult("Skill not found: " + strings.TrimSpace(skillID)), nil
	}
	skill := detail.Skill
	back := skillsSection.command(section)
	if section == "" {
		back = skillsRoot.command()
	}
	picker := NewPickerData(PickerSkill, skillTitle(skill)).
		Meta(skillInfo(skill)).
		Back(back).
		Row("view", "Preview", skill.Description, skillsAction.command(section, skill.ID, "view"))
	if skill.TrustState == skills.TrustTrusted {
		picker.Row("enabled", "Enabled", formatEnabled(skill.Enabled), skillsAction.command(section, skill.ID, "enabled"))
		picker.Row("quarantine", "Move to Quarantine", "", skillsAction.command(section, skill.ID, "quarantine"))
	} else {
		picker.Row("trust-enable", "Trust & Enable", "", skillsAction.command(section, skill.ID, "trust-enable"))
		picker.Row("keep", "Keep Quarantined", "", skillsAction.command(section, skill.ID, "keep"))
	}
	picker.Row("edit", "Edit", "", skillsEdit.command(section, skill.ID))
	if skill.State == skills.StateArchived {
		picker.Row("restore", "Restore", "", skillsAction.command(section, skill.ID, "restore"))
	} else {
		picker.Row("archive", "Archive", "", skillsAction.command(section, skill.ID, "archive"))
	}
	picker.Danger("remove", "Remove", "", skillsAction.command(section, skill.ID, "remove"))
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) skillEditMenu(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return textResult(err.Error()), nil
	}
	picker := NewPickerData(PickerSkillsSection, "Edit "+skillTitle(detail.Skill)).
		Back(skillsOpen.command(section, skillID)).
		Row("metadata", "Metadata", "Name, description, tags, category", skillsAction.command(section, skillID, "edit-metadata")).
		Row("instructions", "Instructions", "SKILL.md body", skillsAction.command(section, skillID, "edit-body"))
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) skillEnabledPicker(ctx context.Context, section string, skillID string) (Result, error) {
	detail, err := d.daemon.GetSkill(ctx, skillID)
	if err != nil {
		return textResult(err.Error()), nil
	}
	return Result{
		Screen: NewPickerData(PickerSkill, "Enabled").
			Meta(skillTitle(detail.Skill)).
			Select(skillsOpen.command(section, skillID)).
			Item(PickerItem{ID: "on", Title: "On", Selected: detail.Skill.Enabled, Command: skillsSetEnabled.command(section, skillID, "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: !detail.Skill.Enabled, Command: skillsSetEnabled.command(section, skillID, "off")}).
			Build(),
	}, nil
}

func (d *Dispatcher) skillSetEnabled(ctx context.Context, section string, skillID string, value string) (Result, error) {
	enabled, ok := parseEnabledChoice(value)
	if !ok {
		return d.skillEnabledPicker(ctx, section, skillID)
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	if err := d.daemon.SkillAction(ctx, skillID, action); err != nil {
		return textResult(err.Error()), nil
	}
	return d.skillPicker(ctx, section, skillID)
}

func (d *Dispatcher) handleLibrarySkillAction(ctx context.Context, section string, skillID string, action string) (Result, error) {
	switch action {
	case "view":
		return d.skillView(ctx, skillID)
	case "enabled":
		return d.skillEnabledPicker(ctx, section, skillID)
	case "trust-enable":
		if err := d.daemon.SkillAction(ctx, skillID, "trust"); err != nil {
			return textResult(err.Error()), nil
		}
		if err := d.daemon.SkillAction(ctx, skillID, "enable"); err != nil {
			return textResult(err.Error()), nil
		}
		return d.skillPicker(ctx, "library", skillID)
	case "enable", "disable", "trust", "quarantine", "archive", "restore", "pin", "unpin":
		if err := d.daemon.SkillAction(ctx, skillID, action); err != nil {
			return textResult(err.Error()), nil
		}
		return d.skillPicker(ctx, section, skillID)
	case "keep":
		return d.skillsSectionPicker(ctx, section, "")
	case "edit-metadata":
		return d.skillEditPrompt(ctx, section, skillID)
	case "edit-body":
		return d.skillBodyEditor(ctx, section, skillID)
	case "remove":
		return d.skillRemoveConfirm(section, skillID), nil
	default:
		return d.skillPicker(ctx, section, skillID)
	}
}
