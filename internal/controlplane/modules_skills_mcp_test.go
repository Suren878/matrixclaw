package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
)

type modulesTestRuntime struct {
	skills          []skills.Skill
	sessionSkills   []skills.Skill
	usedSkill       string
	unloadedSkill   string
	updatedBody     string
	createdName     string
	createdDesc     string
	createdTags     []string
	createdBody     string
	systemMessages  []string
	sentMessages    []string
	mcpConfigUpdate setup.MCPConfigUpdate
	mcpUpdate       setup.MCPServerUpdate
	mcpCreated      setup.MCPServerConfig
	mcpDeleted      string
	mcp             setup.MCPConfig
	browser         setup.BrowserModuleDescriptor
	agents          []core.ExternalAgentDescriptor
}

func (r *modulesTestRuntime) ClientName() string {
	return "test"
}

func (r *modulesTestRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: "s1"}, nil
}

func (r *modulesTestRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return []core.Session{{ID: "s1"}}, nil
}

func (r *modulesTestRuntime) CreateSession(context.Context, string, string, string) (core.Session, error) {
	return core.Session{ID: "s1"}, nil
}

func (r *modulesTestRuntime) UseSession(context.Context, string, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: "s1"}, nil
}

func (r *modulesTestRuntime) RenameSession(context.Context, string, string) (core.Session, error) {
	return core.Session{ID: "s1"}, nil
}

func (r *modulesTestRuntime) DeleteSession(context.Context, string) error {
	return nil
}

func (r *modulesTestRuntime) CreateSystemMessage(_ context.Context, sessionID string, content string) (core.Message, error) {
	r.systemMessages = append(r.systemMessages, sessionID+":"+content)
	return core.Message{SessionID: sessionID, Content: content}, nil
}

func (r *modulesTestRuntime) SendMessage(_ context.Context, sessionID string, content string) (core.AcceptRunResult, error) {
	r.sentMessages = append(r.sentMessages, sessionID+":"+content)
	return core.AcceptRunResult{SessionID: sessionID}, nil
}

func (r *modulesTestRuntime) ListSkills(context.Context, skills.SearchOptions) ([]skills.Skill, error) {
	return append([]skills.Skill(nil), r.skills...), nil
}

func (r *modulesTestRuntime) SearchSkills(context.Context, string, skills.SearchOptions) ([]skills.Skill, error) {
	return append([]skills.Skill(nil), r.skills...), nil
}

func (r *modulesTestRuntime) GetSkill(_ context.Context, id string) (skills.SkillDetail, error) {
	for _, item := range r.skills {
		if item.ID == id {
			return skills.SkillDetail{Skill: item, Body: "body"}, nil
		}
	}
	return skills.SkillDetail{}, errTestNotFound{}
}

func (r *modulesTestRuntime) InstallSkill(context.Context, string) ([]skills.Skill, error) {
	return nil, nil
}

func (r *modulesTestRuntime) SkillAction(context.Context, string, string) error {
	return nil
}

func (r *modulesTestRuntime) SessionSkills(context.Context, string) ([]skills.Skill, error) {
	return append([]skills.Skill(nil), r.sessionSkills...), nil
}

func (r *modulesTestRuntime) UseSkill(context.Context, string, string) (skills.SkillDetail, error) {
	r.usedSkill = "s1"
	if len(r.skills) == 0 {
		return skills.SkillDetail{}, errTestNotFound{}
	}
	return skills.SkillDetail{Skill: r.skills[0], Body: "body"}, nil
}

func (r *modulesTestRuntime) UnloadSkill(_ context.Context, sessionID string, skillID string) error {
	r.unloadedSkill = sessionID + ":" + skillID
	return nil
}

func (r *modulesTestRuntime) CreateSkillDraft(_ context.Context, name string, description string, tags []string, body string) (skills.Skill, error) {
	r.createdName = name
	r.createdDesc = description
	r.createdTags = tags
	r.createdBody = body
	return skills.Skill{ID: "matrix-ui", Name: name, Description: description, Tags: tags, TrustState: skills.TrustQuarantine, State: skills.StateActive}, nil
}

func (r *modulesTestRuntime) UpdateSkillMetadata(context.Context, string, skills.MetadataUpdate) (skills.Skill, error) {
	return skills.Skill{}, nil
}

func (r *modulesTestRuntime) UpdateSkillBody(_ context.Context, _ string, body string) error {
	r.updatedBody = body
	return nil
}

func (r *modulesTestRuntime) SetSkillEnabled(context.Context, string, bool) error {
	return nil
}

func (r *modulesTestRuntime) ListExternalAgents(context.Context) ([]core.ExternalAgentDescriptor, error) {
	return append([]core.ExternalAgentDescriptor(nil), r.agents...), nil
}

func (r *modulesTestRuntime) UpdateExternalAgent(context.Context, string, core.UpdateExternalAgentRequest) ([]core.ExternalAgentDescriptor, error) {
	return append([]core.ExternalAgentDescriptor(nil), r.agents...), nil
}

func (r *modulesTestRuntime) MCPConfig(context.Context) (setup.MCPConfigResponse, error) {
	return setup.MCPConfigResponse{Config: r.mcp}, nil
}

func (r *modulesTestRuntime) UpdateMCPConfig(_ context.Context, update setup.MCPConfigUpdate) (setup.MCPConfigResponse, error) {
	r.mcpConfigUpdate = update
	if update.Enabled != nil {
		r.mcp.Enabled = *update.Enabled
	}
	return setup.MCPConfigResponse{Config: r.mcp}, nil
}

func (r *modulesTestRuntime) CreateMCPServer(_ context.Context, server setup.MCPServerConfig) (setup.MCPConfigResponse, error) {
	r.mcpCreated = server
	r.mcp.Servers = append(r.mcp.Servers, server)
	return setup.MCPConfigResponse{Config: r.mcp}, nil
}

func (r *modulesTestRuntime) UpdateMCPServer(_ context.Context, _ string, update setup.MCPServerUpdate) (setup.MCPConfigResponse, error) {
	r.mcpUpdate = update
	return setup.MCPConfigResponse{Config: r.mcp}, nil
}

func (r *modulesTestRuntime) DeleteMCPServer(_ context.Context, serverID string) (setup.MCPConfigResponse, error) {
	r.mcpDeleted = serverID
	servers := make([]setup.MCPServerConfig, 0, len(r.mcp.Servers))
	for _, server := range r.mcp.Servers {
		if strings.EqualFold(server.ID, serverID) {
			continue
		}
		servers = append(servers, server)
	}
	r.mcp.Servers = servers
	return setup.MCPConfigResponse{Config: r.mcp}, nil
}

func (r *modulesTestRuntime) BrowserModule(context.Context) (setup.BrowserModuleDescriptor, error) {
	return r.browser, nil
}

func (r *modulesTestRuntime) UpdateBrowserModule(context.Context, setup.BrowserModuleUpdate) (setup.BrowserModuleDescriptor, error) {
	return r.browser, nil
}

func (r *modulesTestRuntime) BrowserProviderAction(context.Context, string, setup.BrowserProviderActionRequest) (setup.BrowserProviderOption, error) {
	if len(r.browser.Providers) == 0 {
		return setup.BrowserProviderOption{}, nil
	}
	return r.browser.Providers[0], nil
}

type errTestNotFound struct{}

func (errTestNotFound) Error() string { return "not found" }

func TestModulesPickerIncludesSkillsAndMCP(t *testing.T) {
	runtime := &modulesTestRuntime{
		skills:  []skills.Skill{{ID: "deploy", TrustState: skills.TrustTrusted, State: skills.StateActive}},
		browser: browserModuleForProvider(browserPickerProvider(true)),
		mcp: setup.MCPConfig{
			Enabled: true,
			Servers: []setup.MCPServerConfig{
				{ID: "docs", Enabled: true, Transport: "stdio", Command: "docs-mcp"},
				{ID: "repo", Enabled: false, Transport: "http", Endpoint: "http://127.0.0.1:3333"},
			},
		},
	}
	result, err := New(runtime, "").modulesPicker(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("modulesPicker() Picker = nil")
	}
	if !pickerHasItem(result.Picker, "skills", "Skills") {
		t.Fatalf("modules picker missing Skills: %#v", result.Picker.Items)
	}
	if !pickerHasItem(result.Picker, "mcp", "External MCP") {
		t.Fatalf("modules picker missing External MCP: %#v", result.Picker.Items)
	}
	if !pickerHasItem(result.Picker, "browser", "Browser") {
		t.Fatalf("modules picker missing Browser: %#v", result.Picker.Items)
	}
}

func TestModulesPickerExternalAgentsInfoUsesConfiguredAgents(t *testing.T) {
	runtime := &modulesTestRuntime{
		agents: []core.ExternalAgentDescriptor{
			{ID: "codex-app", DisplayName: "Codex", Installed: true, Enabled: false},
			{ID: "claude-code", DisplayName: "Claude", Installed: true, Enabled: true},
		},
	}
	result, err := New(runtime, "").modulesPicker(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("modulesPicker() Picker = nil")
	}
	agents := requirePickerItem(t, result.Picker, "agents")
	if agents.Info != "Claude" {
		t.Fatalf("External Agents info = %q, want Claude", agents.Info)
	}
}

func TestSkillsPickerBackReturnsToModules(t *testing.T) {
	runtime := &modulesTestRuntime{skills: []skills.Skill{{ID: "deploy", Description: "Deploy helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}}}
	result, err := New(runtime, "").handleSkills(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("handleSkills() Picker = nil")
	}
	if result.Picker.BackCommand != modulesCommand() {
		t.Fatalf("BackCommand = %q, want %q", result.Picker.BackCommand, modulesCommand())
	}
	if result.Picker.CloseCommand != modulesCommand() {
		t.Fatalf("CloseCommand = %q, want %q", result.Picker.CloseCommand, modulesCommand())
	}
}

func TestMCPPickerBackReturnsToModules(t *testing.T) {
	runtime := &modulesTestRuntime{mcp: setup.MCPConfig{Servers: []setup.MCPServerConfig{{ID: "docs", Enabled: true, Transport: "stdio", Command: "docs-mcp"}}}}
	result, err := New(runtime, "").handleMCP(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("handleMCP() Picker = nil")
	}
	if result.Picker.BackCommand != modulesCommand() {
		t.Fatalf("BackCommand = %q, want %q", result.Picker.BackCommand, modulesCommand())
	}
	if result.Picker.CloseCommand != modulesCommand() {
		t.Fatalf("CloseCommand = %q, want %q", result.Picker.CloseCommand, modulesCommand())
	}
}

func TestMCPPickerCanAddDeleteAndHidesBrowser(t *testing.T) {
	runtime := &modulesTestRuntime{mcp: setup.MCPConfig{Enabled: true, Servers: []setup.MCPServerConfig{
		{ID: "docs", Name: "Docs", Enabled: true, Transport: "stdio", Command: "docs-mcp"},
		{ID: "browser", Name: "Local Browser", Enabled: true, Transport: "stdio", Command: "playwright-mcp"},
	}}}
	result, err := New(runtime, "").handleMCP(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("handleMCP() Picker = nil")
	}
	if result.Picker.Title != "External MCP Servers" {
		t.Fatalf("Picker title = %q", result.Picker.Title)
	}
	if !pickerHasItem(result.Picker, "add", "Add Server") {
		t.Fatalf("MCP picker missing Add Server: %#v", result.Picker.Items)
	}
	if pickerHasID(result.Picker, "browser") {
		t.Fatalf("MCP picker should hide managed browser server: %#v", result.Picker.Items)
	}

	result, err = New(runtime, "").handleMCP(context.Background(), "add")
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt == nil || result.Prompt.SubmitCommandPrefix != mcpCommand("create")+" " {
		t.Fatalf("add prompt = %#v", result.Prompt)
	}

	result, err = New(runtime, "").handleMCP(context.Background(), "create Travel Docs")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.mcpCreated.ID != "travel_docs" || runtime.mcpCreated.Command != "travel_docs" {
		t.Fatalf("created MCP server = %#v", runtime.mcpCreated)
	}
	if result.Form == nil {
		t.Fatalf("create should open edit form: %#v", result)
	}

	result, err = New(runtime, "").handleMCP(context.Background(), "docs delete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Confirm == nil || result.Confirm.ConfirmCommand != mcpServerCommand("docs", "delete-confirm") {
		t.Fatalf("delete confirm = %#v", result.Confirm)
	}
	_, err = New(runtime, "").handleMCP(context.Background(), "docs delete-confirm")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.mcpDeleted != "docs" {
		t.Fatalf("mcpDeleted = %q, want docs", runtime.mcpDeleted)
	}
}

func TestSessionSkillsPickerUsesCurrentSessionAndBacksToSkills(t *testing.T) {
	runtime := &modulesTestRuntime{
		skills: []skills.Skill{
			{ID: "deploy", Name: "Deploy", Description: "Deploy helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true},
			{ID: "draft", Description: "Draft helper", TrustState: skills.TrustQuarantine, State: skills.StateActive, Enabled: true},
			{ID: "off", Description: "Disabled helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: false},
		},
		sessionSkills: []skills.Skill{{ID: "deploy", Name: "Deploy", Description: "Deploy helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}},
	}
	result, err := New(runtime, "").handleSessionSkills(context.Background(), "terminal", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("handleSessionSkills() Picker = nil")
	}
	if !pickerHasItem(result.Picker, "deploy", "Deploy") {
		t.Fatalf("session skills picker missing trusted enabled skill: %#v", result.Picker.Items)
	}
	if pickerHasID(result.Picker, "draft") || pickerHasID(result.Picker, "off") {
		t.Fatalf("session skills picker included unavailable skills: %#v", result.Picker.Items)
	}
	detail, err := New(runtime, "").handleSessionSkills(context.Background(), "terminal", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Picker == nil {
		t.Fatal("session skill detail Picker = nil")
	}
	if detail.Picker.BackCommand != sessionSkillsCommand() {
		t.Fatalf("BackCommand = %q, want %q", detail.Picker.BackCommand, sessionSkillsCommand())
	}
	if !pickerHasItem(detail.Picker, "unload", "Unload from This Chat") {
		t.Fatalf("session skill detail missing unload action: %#v", detail.Picker.Items)
	}
}

func TestSkillsRootShowsLibraryAddUsageAndHidesEmptyReview(t *testing.T) {
	runtime := &modulesTestRuntime{skills: []skills.Skill{{ID: "deploy", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}}}
	result, err := New(runtime, "").handleSkills(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("handleSkills() Picker = nil")
	}
	if pickerHasID(result.Picker, "review") {
		t.Fatalf("library root showed review without reviewable skills: %#v", result.Picker.Items)
	}
	for _, item := range []struct {
		id    string
		title string
	}{
		{"library", "Skill Library"},
		{"add", "Add Skill"},
		{"usage", "Usage Status"},
	} {
		if !pickerHasItem(result.Picker, item.id, item.title) {
			t.Fatalf("library root missing %s/%s: %#v", item.id, item.title, result.Picker.Items)
		}
	}
	if pickerHasID(result.Picker, "installed") || pickerHasID(result.Picker, "drafts") || pickerHasID(result.Picker, "create") {
		t.Fatalf("skills root exposed old library buckets: %#v", result.Picker.Items)
	}
}

func TestLibrarySkillsRootShowsReviewWhenSkillNeedsReview(t *testing.T) {
	runtime := &modulesTestRuntime{skills: []skills.Skill{{ID: "draft", TrustState: skills.TrustQuarantine, State: skills.StateActive}}}
	result, err := New(runtime, "").handleSkills(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("handleSkills() Picker = nil")
	}
	if !pickerHasItem(result.Picker, "review", "Review Queue") {
		t.Fatalf("library root missing Review Queue: %#v", result.Picker.Items)
	}
}

func TestSkillLibraryShowsAllSkillsWithStatusButNoSessionActiveDuplication(t *testing.T) {
	runtime := &modulesTestRuntime{skills: []skills.Skill{
		{ID: "created", Name: "Created Skill", Source: "draft", TrustState: skills.TrustQuarantine, State: skills.StateActive, Enabled: true},
		{ID: "imported", Name: "Imported Skill", Source: "github", Description: "test", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true},
		{ID: "disabled", Name: "Disabled Skill", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: false},
	}}
	result, err := New(runtime, "").handleSkills(context.Background(), "library")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("library Picker = nil")
	}
	if result.Picker.BackCommand != skillsCommand() {
		t.Fatalf("BackCommand = %q, want %q", result.Picker.BackCommand, skillsCommand())
	}
	if !pickerHasInfo(result.Picker, "created", "Created") || !pickerHasInfo(result.Picker, "created", "Needs Review") {
		t.Fatalf("created skill status missing: %#v", result.Picker.Items)
	}
	if !pickerHasInfo(result.Picker, "imported", "Imported") || !pickerHasInfo(result.Picker, "imported", "Enabled") {
		t.Fatalf("imported skill status missing: %#v", result.Picker.Items)
	}
	if pickerHasInfo(result.Picker, "imported", "Trusted") || pickerHasInfo(result.Picker, "imported", "test") {
		t.Fatalf("library row included redundant trusted status or description: %#v", result.Picker.Items)
	}
	if pickerHasInfo(result.Picker, "imported", "Active") {
		t.Fatalf("library should not show session Active state: %#v", result.Picker.Items)
	}
	if pickerItemSelected(result.Picker, "imported") {
		t.Fatalf("library should not mark enabled skills as selected/active: %#v", result.Picker.Items)
	}
	if pickerHasID(result.Picker, "search") {
		t.Fatalf("library should use built-in picker search, not a Search row: %#v", result.Picker.Items)
	}
	if !pickerHasSearch(result.Picker, "imported", "test") {
		t.Fatalf("library hidden search text should include description: %#v", result.Picker.Items)
	}
}

func TestSkillDetailUsesCompactRowsAndEditSubmenu(t *testing.T) {
	runtime := &modulesTestRuntime{skills: []skills.Skill{{ID: "deploy", Name: "Deploy", Description: "Deploy helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true, Pinned: true}}}
	result, err := New(runtime, "").handleSkills(context.Background(), "library deploy")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("skill detail Picker = nil")
	}
	if !pickerHasItem(result.Picker, "edit", "Edit") {
		t.Fatalf("skill detail missing Edit item: %#v", result.Picker.Items)
	}
	if pickerHasID(result.Picker, "pin") || pickerHasID(result.Picker, "unpin") || pickerHasID(result.Picker, "edit-body") {
		t.Fatalf("skill detail exposed pin or split edit items: %#v", result.Picker.Items)
	}
	for _, item := range result.Picker.Items {
		if item.ID != "remove" && item.ID != "back" && item.Role == PickerItemRoleAction {
			t.Fatalf("skill detail item %s uses action role causing separators: %#v", item.ID, result.Picker.Items)
		}
	}
	edit, err := New(runtime, "").handleSkills(context.Background(), "library deploy edit")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Picker == nil {
		t.Fatal("edit submenu Picker = nil")
	}
	if edit.Picker.BackCommand != skillsCommand("library", "deploy") {
		t.Fatalf("edit submenu BackCommand = %q", edit.Picker.BackCommand)
	}
	if !pickerHasItem(edit.Picker, "metadata", "Metadata") || !pickerHasItem(edit.Picker, "instructions", "Instructions") {
		t.Fatalf("edit submenu missing rows: %#v", edit.Picker.Items)
	}
}

func TestSkillAddPickerOffersManualAICreationAndImport(t *testing.T) {
	result, err := New(&modulesTestRuntime{}, "").handleSkills(context.Background(), "add")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("add Picker = nil")
	}
	if result.Picker.BackCommand != skillsCommand() {
		t.Fatalf("BackCommand = %q, want %q", result.Picker.BackCommand, skillsCommand())
	}
	for _, item := range []struct {
		id    string
		title string
	}{
		{"manual", "Manual Create"},
		{"ai", "Create with AI"},
		{"import", "Import"},
	} {
		if !pickerHasItem(result.Picker, item.id, item.title) {
			t.Fatalf("add picker missing %s/%s: %#v", item.id, item.title, result.Picker.Items)
		}
	}
}

func TestSkillAICreateStartsCurrentSessionDiscussion(t *testing.T) {
	runtime := &modulesTestRuntime{}
	prompt, err := New(runtime, "").handleSkills(context.Background(), "ai-create")
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Prompt == nil {
		t.Fatalf("ai-create without description should open prompt: %#v", prompt)
	}
	if prompt.Prompt.CancelCommand != skillsCommand("add") {
		t.Fatalf("ai-create prompt CancelCommand = %q", prompt.Prompt.CancelCommand)
	}
	result, err := New(runtime, "").handleSkills(context.Background(), "ai-create Build a skill for Matrix UI edits")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handled || !result.ReloadSnapshot {
		t.Fatalf("ai-create result = %#v", result)
	}
	if len(runtime.systemMessages) != 1 {
		t.Fatalf("system messages = %#v", runtime.systemMessages)
	}
	if len(runtime.sentMessages) != 1 {
		t.Fatalf("sent messages = %#v", runtime.sentMessages)
	}
	if !strings.HasPrefix(runtime.systemMessages[0], "s1:") {
		t.Fatalf("system message did not target current session: %#v", runtime.systemMessages)
	}
	if !strings.Contains(runtime.systemMessages[0], "skill_manage") || !strings.Contains(runtime.systemMessages[0], "quarantine") {
		t.Fatalf("system message missing skill creation guidance: %q", runtime.systemMessages[0])
	}
	if !strings.HasPrefix(runtime.sentMessages[0], "s1:") || !strings.Contains(runtime.sentMessages[0], "Build a skill for Matrix UI edits") {
		t.Fatalf("sent message did not start skill discussion: %#v", runtime.sentMessages)
	}
}

func TestSkillUsageStatusUsesCurrentSession(t *testing.T) {
	runtime := &modulesTestRuntime{
		skills:        []skills.Skill{{ID: "deploy", Name: "Deploy", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}},
		sessionSkills: []skills.Skill{{ID: "deploy", Name: "Deploy", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}},
	}
	result, err := New(runtime, "").handleSkills(context.Background(), "usage")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("usage Picker = nil")
	}
	if result.Picker.BackCommand != skillsCommand() {
		t.Fatalf("BackCommand = %q, want %q", result.Picker.BackCommand, skillsCommand())
	}
	if !pickerHasItem(result.Picker, "deploy", "Deploy") {
		t.Fatalf("usage picker missing active skill: %#v", result.Picker.Items)
	}
}

func TestSkillCreateUsesStepByStepWizard(t *testing.T) {
	runtime := &modulesTestRuntime{}
	dispatcher := New(runtime, "")
	nameStep, err := dispatcher.handleSkills(context.Background(), "create")
	if err != nil {
		t.Fatal(err)
	}
	if nameStep.Prompt == nil || nameStep.Prompt.Title != "Skill Name" {
		t.Fatalf("create step = %#v", nameStep.Prompt)
	}
	descStep, err := dispatcher.handleSkills(context.Background(), "create-name Matrix UI")
	if err != nil {
		t.Fatal(err)
	}
	if descStep.Prompt == nil || descStep.Prompt.Title != "Skill Description" {
		t.Fatalf("description step = %#v", descStep.Prompt)
	}
	descToken := tokenFromPrefix(t, descStep.Prompt.SubmitCommandPrefix)
	tagsStep, err := dispatcher.handleSkills(context.Background(), "create-description "+descToken+" Helps edit Matrix UI")
	if err != nil {
		t.Fatal(err)
	}
	if tagsStep.Prompt == nil || tagsStep.Prompt.Title != "Skill Tags" {
		t.Fatalf("tags step = %#v", tagsStep.Prompt)
	}
	tagsToken := tokenFromPrefix(t, tagsStep.Prompt.SubmitCommandPrefix)
	bodyStep, err := dispatcher.handleSkills(context.Background(), "create-tags "+tagsToken+" ui,matrix")
	if err != nil {
		t.Fatal(err)
	}
	if bodyStep.TextEdit == nil || bodyStep.TextEdit.Title != "Skill Instructions" {
		t.Fatalf("body step = %#v", bodyStep.TextEdit)
	}
	bodyToken := tokenFromPrefix(t, bodyStep.TextEdit.SubmitCommandPrefix)
	_, err = dispatcher.handleSkills(context.Background(), "create-save "+bodyToken+" Use existing pickers.")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.createdName != "Matrix UI" || runtime.createdDesc != "Helps edit Matrix UI" || runtime.createdBody != "Use existing pickers." {
		t.Fatalf("created draft = name %q desc %q body %q", runtime.createdName, runtime.createdDesc, runtime.createdBody)
	}
	if len(runtime.createdTags) != 2 || runtime.createdTags[0] != "ui" || runtime.createdTags[1] != "matrix" {
		t.Fatalf("created tags = %#v", runtime.createdTags)
	}
}

func TestSkillDetailCanOpenLargeBodyEditor(t *testing.T) {
	runtime := &modulesTestRuntime{skills: []skills.Skill{{ID: "deploy", Name: "Deploy", Description: "Deploy helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}}}
	result, err := New(runtime, "").handleSkills(context.Background(), "installed deploy edit-body")
	if err != nil {
		t.Fatal(err)
	}
	if result.TextEdit == nil {
		t.Fatalf("edit-body did not return TextEditData: %#v", result)
	}
	if result.TextEdit.SubmitCommandPrefix != skillsCommandPrefix("installed", "deploy", "save-body") {
		t.Fatalf("SubmitCommandPrefix = %q", result.TextEdit.SubmitCommandPrefix)
	}
	if result.TextEdit.Value != "body" {
		t.Fatalf("TextEdit value = %q", result.TextEdit.Value)
	}
}

func TestMCPServerCanEditConfigViaForm(t *testing.T) {
	runtime := &modulesTestRuntime{mcp: setup.MCPConfig{Servers: []setup.MCPServerConfig{{ID: "docs", Name: "Docs", Enabled: true, Transport: "stdio", Command: "docs-mcp", Args: []string{"serve"}, ToolPrefix: "docs"}}}}
	result, err := New(runtime, "").handleMCP(context.Background(), "docs edit")
	if err != nil {
		t.Fatal(err)
	}
	if result.Form == nil {
		t.Fatalf("mcp edit did not return FormData: %#v", result)
	}
	if result.Form.CancelCommand != mcpServerCommand("docs") {
		t.Fatalf("CancelCommand = %q", result.Form.CancelCommand)
	}
	if len(result.Form.Fields) == 0 || result.Form.Fields[0].EditCommand == "" {
		t.Fatalf("edit form fields missing edit commands: %#v", result.Form.Fields)
	}
}

func pickerHasItem(picker *PickerData, id string, title string) bool {
	for _, item := range picker.Items {
		if item.ID == id && item.Title == title {
			return true
		}
	}
	return false
}

func pickerHasID(picker *PickerData, id string) bool {
	for _, item := range picker.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func pickerHasInfo(picker *PickerData, id string, info string) bool {
	for _, item := range picker.Items {
		if item.ID == id && strings.Contains(item.Info, info) {
			return true
		}
	}
	return false
}

func pickerHasSearch(picker *PickerData, id string, value string) bool {
	for _, item := range picker.Items {
		if item.ID == id && strings.Contains(item.Search, value) {
			return true
		}
	}
	return false
}

func pickerItemSelected(picker *PickerData, id string) bool {
	for _, item := range picker.Items {
		if item.ID == id {
			return item.Selected
		}
	}
	return false
}

func tokenFromPrefix(t *testing.T, prefix string) string {
	t.Helper()
	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		t.Fatalf("empty prefix")
	}
	return fields[len(fields)-1]
}
