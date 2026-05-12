package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type fakeRuntime struct {
	binding            core.ClientBinding
	sessions           []core.Session
	setupProviders     []setup.ProviderSetupItem
	created            int
	configuredProvider string
	configuredAPIKey   string
	configuredUpdate   setup.ProviderSetupUpdate
	modelsProvider     string
	modelsUpdate       setup.ProviderSetupUpdate
	models             []string
	deletedProvider    string
	updatedProvider    string
	updatedPermission  core.PermissionMode
	automationJobs     []automation.Job
	storageFiles       []localstorage.Entry
	tempStorageFiles   []localstorage.TempEntry
	deletedStoragePath string
	restarted          bool
	compacted          bool
}

func (f *fakeRuntime) ClientName() string {
	return "test"
}

func (f *fakeRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return f.binding, nil
}

func (f *fakeRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return append([]core.Session(nil), f.sessions...), nil
}

func (f *fakeRuntime) CreateSession(_ context.Context, _ string, title string, _ string) (core.Session, error) {
	f.created++
	session := core.Session{ID: "session_new", Title: title}
	f.sessions = append(f.sessions, session)
	f.binding.SessionID = session.ID
	return session, nil
}

func (f *fakeRuntime) UseSession(_ context.Context, _ string, sessionID string) (core.ClientBinding, error) {
	f.binding.SessionID = sessionID
	return f.binding, nil
}

func (f *fakeRuntime) RenameSession(_ context.Context, sessionID string, title string) (core.Session, error) {
	for i := range f.sessions {
		if f.sessions[i].ID == sessionID {
			f.sessions[i].Title = title
			return f.sessions[i], nil
		}
	}
	return core.Session{}, core.ErrNotFound
}

func (f *fakeRuntime) DeleteSession(_ context.Context, sessionID string) error {
	next := f.sessions[:0]
	found := false
	for _, session := range f.sessions {
		if session.ID == sessionID {
			found = true
			continue
		}
		next = append(next, session)
	}
	f.sessions = next
	if !found {
		return core.ErrNotFound
	}
	return nil
}

func (f *fakeRuntime) SessionContext(context.Context, string) (core.ContextReport, error) {
	return core.ContextReport{
		Estimated:     true,
		TokenEstimate: 1234,
		Blocks: []core.ContextBlock{{
			ID:            "messages",
			Kind:          core.ContextBlockMessages,
			TokenEstimate: 1234,
			Included:      true,
		}},
	}, nil
}

func (f *fakeRuntime) CompactSession(context.Context, string) (core.CompactSessionResult, error) {
	f.compacted = true
	return core.CompactSessionResult{
		Message: core.Message{
			ID:      "msg_compact",
			Role:    core.MessageRoleSystem,
			Content: "Context compacted\n\nSummary.",
		},
		Context: core.ContextReport{Estimated: true, TokenEstimate: 42},
	}, nil
}

func (f *fakeRuntime) CreateSystemMessage(_ context.Context, sessionID string, content string) (core.Message, error) {
	return core.Message{ID: "msg_system", SessionID: sessionID, Role: core.MessageRoleSystem, Content: content}, nil
}

func (f *fakeRuntime) SaveStorageFile(_ context.Context, storagePath string, content []byte, title string, tags []string, mimeType string) (localstorage.Entry, error) {
	entry := localstorage.Entry{Path: storagePath, Title: title, Tags: tags, MIMEType: mimeType, Size: int64(len(content))}
	f.storageFiles = append(f.storageFiles, entry)
	return entry, nil
}

func (f *fakeRuntime) ListTemporaryStorageFiles(context.Context, int) (localstorage.TempListResult, error) {
	return localstorage.TempListResult{Root: "/tmp/storage/temporary", Files: f.tempStorageFiles}, nil
}

func (f *fakeRuntime) PromoteTemporaryStorageFile(_ context.Context, tempPath string, destPath string) (localstorage.Entry, error) {
	if destPath == "" {
		destPath = tempPath
	}
	entry := localstorage.Entry{Path: destPath}
	f.storageFiles = append(f.storageFiles, entry)
	return entry, nil
}

func (f *fakeRuntime) DeleteTemporaryStorageFile(_ context.Context, tempPath string) (localstorage.TempEntry, error) {
	return localstorage.TempEntry{Path: tempPath}, nil
}

func (f *fakeRuntime) CleanupTemporaryStorageFiles(context.Context) (localstorage.CleanupResult, error) {
	return localstorage.CleanupResult{DeletedFiles: 1, FreedBytes: 12}, nil
}

func (f *fakeRuntime) UpdateTemporaryStorageSettings(_ context.Context, autoCleanup *bool, ttlDays int64, maxGB float64) (localstorage.TempSettings, error) {
	enabled := true
	if autoCleanup != nil {
		enabled = *autoCleanup
	}
	return localstorage.TempSettings{AutoCleanup: enabled, TTLSeconds: ttlDays * 24 * 3600, MaxBytes: int64(maxGB * 1024 * 1024 * 1024)}, nil
}

func (f *fakeRuntime) ListStorageFiles(context.Context, localstorage.ListFilter) (localstorage.ListResult, error) {
	return localstorage.ListResult{Root: "/tmp/storage", Files: f.storageFiles}, nil
}

func (f *fakeRuntime) ReadStorageFile(_ context.Context, storagePath string) (localstorage.ReadResult, error) {
	for _, file := range f.storageFiles {
		if file.Path == storagePath {
			return localstorage.ReadResult{File: file, Content: "stored content"}, nil
		}
	}
	return localstorage.ReadResult{File: localstorage.Entry{Path: storagePath}, Content: "stored content"}, nil
}

func (f *fakeRuntime) DeleteStorageFile(_ context.Context, storagePath string) (localstorage.Entry, error) {
	f.deletedStoragePath = storagePath
	return localstorage.Entry{Path: storagePath}, nil
}

func (f *fakeRuntime) ListSetupProviders(context.Context) ([]setup.ProviderSetupItem, error) {
	if f.setupProviders != nil {
		return append([]setup.ProviderSetupItem(nil), f.setupProviders...), nil
	}
	return []setup.ProviderSetupItem{
		{ID: "openai", Name: "OpenAI", Status: "Configured · gpt-5.4 · Active", Configured: true, Active: true, Implemented: true},
		{ID: "anthropic", Name: "Anthropic", Status: "Available", Implemented: true},
	}, nil
}

func (f *fakeRuntime) ConfigureSetupProvider(_ context.Context, providerID string, update setup.ProviderSetupUpdate) (setup.ProviderSetupItem, error) {
	f.configuredProvider = providerID
	f.configuredAPIKey = update.APIKey
	f.configuredUpdate = update
	name := update.Name
	if strings.TrimSpace(name) == "" {
		name = "Anthropic"
	}
	return setup.ProviderSetupItem{ID: providerID, Name: name, Model: update.Model, Configured: true, Active: true, Implemented: true}, nil
}

func (f *fakeRuntime) ProviderModels(_ context.Context, providerID string, update setup.ProviderSetupUpdate) ([]string, error) {
	f.modelsProvider = providerID
	f.modelsUpdate = update
	if f.models != nil {
		return append([]string(nil), f.models...), nil
	}
	return []string{"gpt-current", "gpt-alt"}, nil
}

func (f *fakeRuntime) DeleteSetupProvider(_ context.Context, providerID string) error {
	f.deletedProvider = providerID
	next := f.setupProviders[:0]
	for _, provider := range f.setupProviders {
		if provider.ID == providerID {
			continue
		}
		next = append(next, provider)
	}
	f.setupProviders = next
	return nil
}

func (f *fakeRuntime) ServerStatus(context.Context) (core.ServerStatus, error) {
	return core.ServerStatus{
		UptimeSeconds:        3661,
		ProcessRSSBytes:      26 * 1024 * 1024,
		GoAllocBytes:         4 * 1024 * 1024,
		GoSysBytes:           12 * 1024 * 1024,
		MemoryTotalBytes:     1024 * 1024 * 1024,
		MemoryAvailableBytes: 512 * 1024 * 1024,
		MemoryUsedBytes:      512 * 1024 * 1024,
		CPUUsedPercent:       21,
		CPUKnown:             true,
	}, nil
}

func (f *fakeRuntime) RestartDaemon(context.Context) error {
	f.restarted = true
	return nil
}

func (f *fakeRuntime) CreateAutomationJob(_ context.Context, input automation.CreateJobInput) (automation.Job, error) {
	return automation.Job{ID: "job_1", Status: automation.JobStatusActive, Title: input.Prompt, NextDueAt: input.RunAt}, nil
}

func (f *fakeRuntime) ListAutomationJobs(context.Context) ([]automation.Job, error) {
	return append([]automation.Job(nil), f.automationJobs...), nil
}

func (f *fakeRuntime) PauseAutomationJob(_ context.Context, jobID string) (automation.Job, error) {
	return f.setAutomationJobStatus(jobID, automation.JobStatusPaused)
}

func (f *fakeRuntime) ResumeAutomationJob(_ context.Context, jobID string) (automation.Job, error) {
	return f.setAutomationJobStatus(jobID, automation.JobStatusActive)
}

func (f *fakeRuntime) CompleteAutomationJob(_ context.Context, jobID string) (automation.Job, error) {
	return f.setAutomationJobStatus(jobID, automation.JobStatusCompleted)
}

func (f *fakeRuntime) DeleteAutomationJob(_ context.Context, jobID string) (automation.Job, error) {
	return f.setAutomationJobStatus(jobID, automation.JobStatusDeleted)
}

func (f *fakeRuntime) RunAutomationJobNow(context.Context, string) (automation.Fire, error) {
	return automation.Fire{}, nil
}

func (f *fakeRuntime) setAutomationJobStatus(jobID string, status automation.JobStatus) (automation.Job, error) {
	for i := range f.automationJobs {
		if f.automationJobs[i].ID == jobID {
			f.automationJobs[i].Status = status
			return f.automationJobs[i], nil
		}
	}
	return automation.Job{}, core.ErrNotFound
}

func (f *fakeRuntime) UpdateSessionProvider(_ context.Context, sessionID string, providerID string) (core.Session, error) {
	f.updatedProvider = providerID
	for i := range f.sessions {
		if f.sessions[i].ID == sessionID {
			f.sessions[i].ProviderID = providerID
			return f.sessions[i], nil
		}
	}
	return core.Session{ID: sessionID, ProviderID: providerID}, nil
}

func (f *fakeRuntime) UpdateSessionPermissionMode(_ context.Context, sessionID string, mode core.PermissionMode) (core.Session, error) {
	f.updatedPermission = core.NormalizePermissionMode(string(mode))
	for i := range f.sessions {
		if f.sessions[i].ID == sessionID {
			f.sessions[i].PermissionMode = f.updatedPermission
			return f.sessions[i], nil
		}
	}
	return core.Session{ID: sessionID, PermissionMode: f.updatedPermission}, nil
}

func testRuntimeWithSession(session core.Session) *fakeRuntime {
	if strings.TrimSpace(session.ID) == "" {
		session.ID = "session_1"
	}
	if strings.TrimSpace(session.Title) == "" {
		session.Title = "Docs"
	}
	return &fakeRuntime{
		binding:  core.ClientBinding{SessionID: session.ID},
		sessions: []core.Session{session},
	}
}

func testDispatcher(rt *fakeRuntime) *Dispatcher {
	return New(rt, "/tmp")
}

func TestDispatcherSessionPickersAndRenamePrompt(t *testing.T) {
	rt := &fakeRuntime{
		binding: core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{
			{ID: "session_1", Title: "Docs"},
			{ID: "session_2", Title: "Ops"},
		},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/sessions")
	if err != nil {
		t.Fatalf("Handle(/sessions) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Kind != PickerSessions {
		t.Fatalf("expected sessions picker, got %#v", result.Picker)
	}
	if first := result.Picker.Items[0]; first.ID != "new" || first.Title != "New Session" || first.Info != "" {
		t.Fatalf("first picker item = %#v, want create new without info", first)
	}
	if !result.Picker.Items[0].IsAction() {
		t.Fatalf("create new item role = %q, want action", result.Picker.Items[0].Role)
	}
	if !result.Picker.Items[1].Selected {
		t.Fatalf("expected current session item to stay selected: %#v", result.Picker.Items[1])
	}
	if last := result.Picker.Items[len(result.Picker.Items)-1]; last.ID != "cancel" || !last.IsCancel() {
		t.Fatalf("last picker item = %#v, want cancel", last)
	}

	result, err = d.Handle(context.Background(), "local", "/session menu session_1")
	if err != nil {
		t.Fatalf("Handle(menu) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Kind != PickerSessionActions || result.Picker.ContextID != "session_1" {
		t.Fatalf("Handle(menu) picker = %#v", result.Picker)
	}

	result, err = d.Handle(context.Background(), "local", "/session rename session_1")
	if err != nil {
		t.Fatalf("Handle(rename prompt) error = %v", err)
	}
	if result.Prompt == nil || result.Prompt.SubmitCommandPrefix != "/session rename session_1 " {
		t.Fatalf("rename prompt = %#v", result.Prompt)
	}
}

func TestDispatcherSharedCommandSmoke(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	rt := &fakeRuntime{
		binding: core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{
			{ID: "session_1", Title: "Docs", ProviderID: "openai", ModelID: "gpt-current"},
		},
		automationJobs: []automation.Job{
			{ID: "job_active", Status: automation.JobStatusActive, Title: "Check server", NextDueAt: &now},
			{ID: "job_done", Status: automation.JobStatusCompleted, Title: "Old reminder"},
		},
	}
	d := New(rt, "/tmp")
	d.now = func() time.Time { return now }

	commands := []string{
		"/sessions",
		"/provider",
		"/provider custom",
		"/permissions",
		"/tasks",
		"/tasks archive",
		"/server",
		"/status",
		"/help",
		"/remind in 1m -- go to the dentist",
	}
	for _, command := range commands {
		result, err := d.Handle(context.Background(), "telegram:42", command)
		if err != nil {
			t.Fatalf("Handle(%q) error = %v", command, err)
		}
		if !result.Handled {
			t.Fatalf("Handle(%q) was not handled", command)
		}
		if result.Picker != nil {
			assertPickerContract(t, *result.Picker)
		}
		if result.Text == "" && result.Picker == nil && result.Prompt == nil && result.Confirm == nil {
			t.Fatalf("Handle(%q) returned no renderable output", command)
		}
	}
}

func assertPickerContract(t *testing.T, picker PickerData) {
	t.Helper()
	for _, item := range picker.Items {
		switch item.ID {
		case "cancel":
			if !item.IsCancel() {
				t.Fatalf("%s picker cancel item missing cancel role: %#v", picker.Kind, item)
			}
		case "back":
			if !item.IsBack() {
				t.Fatalf("%s picker back item missing back role: %#v", picker.Kind, item)
			}
		case "new", "custom":
			if !item.IsAction() {
				t.Fatalf("%s picker action item missing action role: %#v", picker.Kind, item)
			}
		case "delete", "delete_closed":
			if !item.IsDanger() {
				t.Fatalf("%s picker danger item missing danger role: %#v", picker.Kind, item)
			}
		}
	}
}

func TestDispatcherPermissionsPickerUsesCompactLabels(t *testing.T) {
	rt := &fakeRuntime{
		binding:  core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{{ID: "session_1", Title: "Docs", PermissionMode: core.PermissionModeAcceptEdits}},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/permissions")
	if err != nil {
		t.Fatalf("Handle(/permissions) error = %v", err)
	}
	if result.Picker == nil {
		t.Fatal("expected permissions picker")
	}
	want := []struct {
		title   string
		info    string
		command string
	}{
		{title: "Ask First", info: "", command: "/permissions default"},
		{title: "Edits Only", info: "", command: "/permissions accept_edits"},
		{title: "Full Auto", info: "", command: "/permissions full_auto"},
	}
	for i, want := range want {
		if result.Picker.Items[i].Title != want.title {
			t.Fatalf("item[%d].Title = %q, want %q", i, result.Picker.Items[i].Title, want.title)
		}
		if result.Picker.Items[i].Info != want.info {
			t.Fatalf("item[%d].Info = %q, want %q", i, result.Picker.Items[i].Info, want.info)
		}
		if result.Picker.Items[i].Command != want.command {
			t.Fatalf("item[%d].Command = %q, want %q", i, result.Picker.Items[i].Command, want.command)
		}
	}
	if got := PickerCommandFor(PickerPermissions, "", "full_auto"); got != "/permissions full_auto" {
		t.Fatalf("permissions callback fallback = %q, want /permissions full_auto", got)
	}
	if !result.Picker.Items[1].Selected {
		t.Fatalf("expected Auto Accept item selected: %#v", result.Picker.Items[1])
	}
	for _, item := range result.Picker.Items {
		if item.IsBack() || item.IsCancel() {
			t.Fatalf("permissions picker should not include visible back/cancel item: %#v", result.Picker.Items)
		}
	}
}

func TestDispatcherStorageLabelsStayClean(t *testing.T) {
	rt := &fakeRuntime{
		storageFiles: []localstorage.Entry{{Path: "docs/smoke.md", Title: "Smoke", Size: 12}},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/modules storage files")
	if err != nil {
		t.Fatalf("Handle(/modules storage files) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Title != "Stored Files" {
		t.Fatalf("stored files picker = %#v", result.Picker)
	}

	result, err = d.Handle(context.Background(), "local", "/modules storage import")
	if err != nil {
		t.Fatalf("Handle(/modules storage import) error = %v", err)
	}
	if result.Prompt == nil || result.Prompt.Title != "Local File Path" || result.Prompt.Placeholder != "/absolute/path/to/file.txt" {
		t.Fatalf("import prompt = %#v", result.Prompt)
	}
}

func TestDispatcherStorageAutoCleanupUsesPicker(t *testing.T) {
	rt := &fakeRuntime{}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/modules storage temp-cleanup-mode")
	if err != nil {
		t.Fatalf("Handle(/modules storage temp-cleanup-mode) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Kind != PickerStorageCleanup || result.Picker.Title != "Auto Cleanup" {
		t.Fatalf("auto cleanup picker = %#v", result.Picker)
	}
	if len(result.Picker.Items) < 2 || result.Picker.Items[0].ID != "on" || result.Picker.Items[1].ID != "off" {
		t.Fatalf("auto cleanup items = %#v", result.Picker.Items)
	}
}

func TestDispatcherDefaultSessionTitleUsesClientChannel(t *testing.T) {
	rt := &fakeRuntime{}
	d := New(rt, "/tmp")
	d.now = func() time.Time { return time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC) }

	result, err := d.Handle(context.Background(), "telegram:42", "/new")
	if err != nil {
		t.Fatalf("Handle(/new) error = %v", err)
	}
	if !result.Handled {
		t.Fatal("expected command to be handled")
	}
	if len(rt.sessions) != 1 || rt.sessions[0].Title != "Telegram chat 2026-04-23 17:00" {
		t.Fatalf("created sessions = %#v", rt.sessions)
	}
}

func TestDispatcherTasksPickerSplitsActiveAndArchive(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	rt := &fakeRuntime{
		automationJobs: []automation.Job{
			{ID: "job_active", Status: automation.JobStatusActive, Title: "Check the production server health and report only problems when the response is not healthy", NextDueAt: &now},
			{ID: "job_done", Status: automation.JobStatusCompleted, Title: "Dentist reminder"},
		},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/tasks")
	if err != nil {
		t.Fatalf("Handle(/tasks) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Kind != PickerTasks {
		t.Fatalf("picker = %#v, want tasks picker", result.Picker)
	}
	if result.Picker.Items[0].ID != "open:job_active" {
		t.Fatalf("first task item = %#v, want active task", result.Picker.Items[0])
	}
	if !strings.HasSuffix(result.Picker.Items[0].Title, "...") {
		t.Fatalf("active task title = %q, want truncated title", result.Picker.Items[0].Title)
	}
	if result.Picker.Items[1].ID != "archive" || result.Picker.Items[1].Title != "Archive" || result.Picker.Items[1].Info != "1 completed" {
		t.Fatalf("archive item = %#v, want archive count", result.Picker.Items[1])
	}
}

func TestDispatcherTaskActionsCanArchiveAndDeleteClosed(t *testing.T) {
	rt := &fakeRuntime{
		automationJobs: []automation.Job{
			{ID: "job_active", Status: automation.JobStatusActive, Title: "Check server"},
			{ID: "job_done", Status: automation.JobStatusCompleted, Title: "Old task"},
		},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/tasks menu job_active")
	if err != nil {
		t.Fatalf("Handle(/tasks menu) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Kind != PickerTaskActions {
		t.Fatalf("picker = %#v, want task actions", result.Picker)
	}
	if result.Picker.Items[1].ID != "archive" || result.Picker.Items[1].Title != "Done" {
		t.Fatalf("archive action = %#v", result.Picker.Items[1])
	}

	if _, err := d.Handle(context.Background(), "local", "/tasks complete job_active"); err != nil {
		t.Fatalf("Handle(/tasks complete) error = %v", err)
	}
	if rt.automationJobs[0].Status != automation.JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", rt.automationJobs[0].Status)
	}

	result, err = d.Handle(context.Background(), "local", "/tasks delete-closed")
	if err != nil {
		t.Fatalf("Handle(/tasks delete-closed) error = %v", err)
	}
	if result.Confirm == nil || result.Confirm.ConfirmCommand != "/tasks delete-closed-confirm" {
		t.Fatalf("delete closed confirm = %#v", result.Confirm)
	}

	result, err = d.Handle(context.Background(), "local", "/tasks delete-closed-confirm")
	if err != nil {
		t.Fatalf("Handle(/tasks delete-closed-confirm) error = %v", err)
	}
	if !strings.Contains(result.Text, "Deleted closed tasks: 2") {
		t.Fatalf("delete closed result = %q", result.Text)
	}
	for _, job := range rt.automationJobs {
		if job.Status != automation.JobStatusDeleted {
			t.Fatalf("job %s status = %q, want deleted", job.ID, job.Status)
		}
	}
}

func TestDispatcherProviderOpensProviderScopedFormForMissingAPIKey(t *testing.T) {
	rt := testRuntimeWithSession(core.Session{ProviderID: "openai"})
	d := testDispatcher(rt)

	result, err := d.Handle(context.Background(), "local", "/provider anthropic")
	if err != nil {
		t.Fatalf("Handle(/provider anthropic) error = %v", err)
	}
	if result.Form == nil {
		t.Fatal("expected provider-scoped setup form")
	}
	if customFormFieldValue(t, result.Form, "model") == "" {
		t.Fatalf("provider setup form = %#v, want model field", result.Form)
	}
	if customFormFieldValue(t, result.Form, "key") != "Required" {
		t.Fatalf("provider setup form = %#v, want required API key field", result.Form)
	}
	if formHasField(result.Form, "reasoning") {
		t.Fatalf("anthropic provider form should not expose reasoning: %#v", result.Form.Fields)
	}
	if formHasField(result.Form, "tools") {
		t.Fatalf("anthropic provider form should not expose tool use: %#v", result.Form.Fields)
	}
	for _, field := range result.Form.Fields {
		if field.ID == "name" || field.ID == "base" {
			t.Fatalf("built-in provider form should not expose %q field: %#v", field.ID, result.Form.Fields)
		}
	}
}

func TestDispatcherConfiguredBuiltInProviderUsesCapabilityScopedEditForm(t *testing.T) {
	rt := testRuntimeWithSession(core.Session{ProviderID: "openai", ModelID: "gpt-current"})
	rt.setupProviders = []setup.ProviderSetupItem{
		{
			ID:            "openai",
			CatalogID:     "openai",
			Name:          "OpenAI",
			Type:          providers.TypeOpenAICompat,
			DefaultModel:  "gpt-5.4",
			APIKeyPreview: "****1234",
			Configured:    true,
			Active:        true,
			Implemented:   true,
		},
		{ID: "anthropic", CatalogID: "anthropic", Name: "Anthropic", Type: providers.TypeAnthropic, Implemented: true},
	}
	rt.models = []string{"gpt-current", "gpt-next"}
	d := testDispatcher(rt)

	result, err := d.Handle(context.Background(), "local", "/provider openai")
	if err != nil {
		t.Fatalf("Handle(/provider openai) error = %v", err)
	}
	if result.Form == nil {
		t.Fatal("expected provider edit form")
	}
	if formHasField(result.Form, "name") || formHasField(result.Form, "base") {
		t.Fatalf("built-in provider form should not expose identity fields: %#v", result.Form.Fields)
	}
	if got := customFormFieldValue(t, result.Form, "reasoning"); got != "medium" {
		t.Fatalf("reasoning field = %q, want medium", got)
	}
	if got := customFormFieldValue(t, result.Form, "tools"); got != "Enabled" {
		t.Fatalf("tool use field = %q, want Enabled", got)
	}
	if got := customFormFieldValue(t, result.Form, "key"); got != "****1234" {
		t.Fatalf("api key field = %q, want stored preview", got)
	}

	result, err = d.Handle(context.Background(), "local", customFormFieldCommand(t, result.Form, "model"))
	if err != nil {
		t.Fatalf("Handle(model field) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Title != "Model" {
		t.Fatalf("model picker = %#v", result.Picker)
	}
	if rt.modelsProvider != "openai" || rt.modelsUpdate.Model != "gpt-5.4" {
		t.Fatalf("models request = provider %q update %#v", rt.modelsProvider, rt.modelsUpdate)
	}
	if got := pickerItemCommand(t, result.Picker, "gpt-next"); !strings.Contains(got, "/provider edit set model openai ") || !strings.HasSuffix(got, " gpt-next") {
		t.Fatalf("model item command = %q", got)
	}
}

func TestDispatcherQwenProviderFormUsesEndpointPickerAndStackBack(t *testing.T) {
	rt := testRuntimeWithSession(core.Session{ProviderID: "qwen", ModelID: "qwen-plus"})
	rt.setupProviders = []setup.ProviderSetupItem{
		{
			ID:             "qwen",
			CatalogID:      "qwen",
			Name:           "Qwen / DashScope",
			Type:           providers.TypeOpenAICompat,
			BaseURL:        "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
			Model:          "qwen-plus",
			DefaultModel:   "qwen-plus",
			APIKeyPreview:  "****qwen",
			Configured:     true,
			Active:         true,
			Implemented:    true,
			Capabilities:   providers.Capabilities{ModelDiscovery: true, ToolCalling: true},
			BaseURLOptions: qwenBaseURLOptionsForTest(),
		},
	}
	d := testDispatcher(rt)

	result, err := d.Handle(context.Background(), "local", "/provider qwen")
	if err != nil {
		t.Fatalf("Handle(/provider qwen) error = %v", err)
	}
	if result.Form == nil {
		t.Fatal("expected qwen provider form")
	}
	if got := customFormFieldValue(t, result.Form, "base"); got != "Singapore / International" {
		t.Fatalf("qwen endpoint field = %q, want Singapore / International", got)
	}
	if result.Form.CancelCommand != "" {
		t.Fatalf("cancel command = %q, want empty stack back", result.Form.CancelCommand)
	}
	form := result.Form

	result, err = d.Handle(context.Background(), "local", customFormFieldCommand(t, result.Form, "base"))
	if err != nil {
		t.Fatalf("Handle(qwen base field) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Title != "Edit Qwen / DashScope: endpoint" {
		t.Fatalf("endpoint picker = %#v", result.Picker)
	}
	if got := pickerItemCommand(t, result.Picker, "china-beijing"); !strings.HasSuffix(got, " https://dashscope.aliyuncs.com/compatible-mode/v1") {
		t.Fatalf("china endpoint command = %q", got)
	}

	result, err = d.Handle(context.Background(), "local", form.SubmitCommand)
	if err != nil {
		t.Fatalf("Handle(qwen save) error = %v", err)
	}
	if result.Form == nil {
		t.Fatal("save should return updated provider form")
	}
	if result.Form.Error != "Saved." {
		t.Fatalf("save form error/status = %q, want Saved.", result.Form.Error)
	}
	if !result.ReloadSnapshot {
		t.Fatal("save should request snapshot reload")
	}
}

func TestDispatcherProviderPickerOnlyAnnotatesConfiguredProviders(t *testing.T) {
	rt := testRuntimeWithSession(core.Session{ProviderID: "openai", ModelID: "gpt-current"})
	d := testDispatcher(rt)

	result, err := d.Handle(context.Background(), "local", "/provider")
	if err != nil {
		t.Fatalf("Handle(/provider) error = %v", err)
	}
	if result.Picker == nil {
		t.Fatal("expected provider picker")
	}
	for _, item := range result.Picker.Items {
		switch item.ID {
		case "openai":
			if strings.Contains(item.Info, "Configured") {
				t.Fatalf("configured provider info = %q, should not contain Configured", item.Info)
			}
			if item.Info != "gpt-current" {
				t.Fatalf("configured provider info = %q, want current session model", item.Info)
			}
		case "anthropic":
			if item.Info != "" {
				t.Fatalf("available provider info = %q, want empty", item.Info)
			}
			if item.Title != "Anthropic" {
				t.Fatalf("available provider title = %q, want Anthropic", item.Title)
			}
		case "custom":
			if item.Title != "Custom Provider" {
				t.Fatalf("custom provider item = %#v", item)
			}
		}
	}
}

func TestDispatcherProviderKeyConfiguresAndSwitchesSession(t *testing.T) {
	rt := testRuntimeWithSession(core.Session{ProviderID: "openai"})
	d := testDispatcher(rt)

	result, err := d.Handle(context.Background(), "local", "/provider key anthropic sk-secret")
	if err != nil {
		t.Fatalf("Handle(/provider key) error = %v", err)
	}
	if !result.ReloadSnapshot {
		t.Fatal("expected snapshot reload")
	}
	if rt.configuredProvider != "anthropic" || rt.configuredAPIKey != "sk-secret" {
		t.Fatalf("configured provider/key = %q/%q", rt.configuredProvider, rt.configuredAPIKey)
	}
	if rt.updatedProvider != "anthropic" {
		t.Fatalf("updatedProvider = %q, want anthropic", rt.updatedProvider)
	}
}

func TestDispatcherCustomProviderFlow(t *testing.T) {
	rt := testRuntimeWithSession(core.Session{ProviderID: "openai"})
	d := testDispatcher(rt)

	result, err := d.Handle(context.Background(), "local", "/provider custom")
	if err != nil {
		t.Fatalf("Handle(/provider custom) error = %v", err)
	}
	if result.Picker == nil || result.Picker.Kind != PickerProviderCustom {
		t.Fatalf("picker = %#v, want provider custom picker", result.Picker)
	}

	result, err = d.Handle(context.Background(), "local", "/provider custom openai")
	if err != nil {
		t.Fatalf("Handle(/provider custom openai) error = %v", err)
	}
	if result.Form == nil || result.Form.Title != "Custom OpenAI-Compatible" {
		t.Fatalf("custom provider form = %#v, want form", result.Form)
	}
	if formHasField(result.Form, "reasoning") {
		t.Fatalf("generic custom OpenAI-compatible form should not expose reasoning: %#v", result.Form.Fields)
	}
	if customFormFieldValue(t, result.Form, "tools") != "Enabled" {
		t.Fatalf("tool mode field = %#v", result.Form.Fields)
	}

	result, err = d.Handle(context.Background(), "local", customFormFieldCommand(t, result.Form, "key"))
	if err != nil {
		t.Fatalf("Handle(custom provider key edit) error = %v", err)
	}
	if result.Prompt == nil || !result.Prompt.Sensitive {
		t.Fatalf("api key prompt = %#v, want sensitive key prompt", result.Prompt)
	}

	token := encodeCustomProviderFormToken(customProviderForm{
		Name:        "Local AI",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Model:       "llama3",
		APIKey:      "test-api-key",
		ToolUseMode: providers.ToolUseDisabled,
	})
	result, err = d.Handle(context.Background(), "local", customProviderCommand("openai", "save", token))
	if err != nil {
		t.Fatalf("Handle(custom provider save) error = %v", err)
	}
	if result.Confirm == nil || !strings.Contains(result.Confirm.ConfirmCommand, "/provider custom openai save-confirm ") {
		t.Fatalf("save confirm = %#v", result.Confirm)
	}
	result, err = d.Handle(context.Background(), "local", result.Confirm.ConfirmCommand)
	if err != nil {
		t.Fatalf("Handle(custom provider form) error = %v", err)
	}
	if rt.configuredProvider != "local-ai" {
		t.Fatalf("configuredProvider = %q, want local-ai", rt.configuredProvider)
	}
	if rt.configuredUpdate.Type != providers.TypeOpenAICompat || rt.configuredUpdate.BaseURL != "http://127.0.0.1:11434/v1" || rt.configuredUpdate.Model != "llama3" || rt.configuredUpdate.APIKey != "test-api-key" {
		t.Fatalf("configured update = %#v", rt.configuredUpdate)
	}
	if rt.configuredUpdate.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("configured tool mode = %#v", rt.configuredUpdate)
	}
	if rt.updatedProvider != "local-ai" {
		t.Fatalf("updatedProvider = %q, want local-ai", rt.updatedProvider)
	}
	if !result.ReloadSnapshot {
		t.Fatal("expected reload snapshot after configuring custom provider")
	}
}

func customFormFieldCommand(t *testing.T, form *FormData, id string) string {
	t.Helper()
	if form == nil {
		t.Fatal("form is nil")
	}
	for _, field := range form.Fields {
		if field.ID == id {
			return field.EditCommand
		}
	}
	t.Fatalf("form field %q not found in %#v", id, form.Fields)
	return ""
}

func customFormFieldValue(t *testing.T, form *FormData, id string) string {
	t.Helper()
	if form == nil {
		t.Fatal("form is nil")
	}
	for _, field := range form.Fields {
		if field.ID == id {
			return field.Value
		}
	}
	t.Fatalf("form field %q not found in %#v", id, form.Fields)
	return ""
}

func formHasField(form *FormData, id string) bool {
	if form == nil {
		return false
	}
	for _, field := range form.Fields {
		if field.ID == id {
			return true
		}
	}
	return false
}

func pickerItemCommand(t *testing.T, picker *PickerData, id string) string {
	t.Helper()
	if picker == nil {
		t.Fatal("picker is nil")
	}
	for _, item := range picker.Items {
		if item.ID == id {
			return item.Command
		}
	}
	t.Fatalf("picker item %q not found in %#v", id, picker.Items)
	return ""
}

func qwenBaseURLOptionsForTest() []providers.BaseURLOption {
	entry, _ := providers.CatalogEntryByID("qwen")
	return append([]providers.BaseURLOption(nil), entry.BaseURLOptions...)
}

func TestDispatcherCustomProviderActionsEditAndDelete(t *testing.T) {
	rt := &fakeRuntime{
		binding: core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{
			{ID: "session_1", Title: "Docs", ProviderID: "local-ai"},
		},
		setupProviders: []setup.ProviderSetupItem{
			{ID: "local-ai", CatalogID: "local-ai", Name: "Local AI", Type: providers.TypeOpenAICompat, Capabilities: providers.Capabilities{ToolCalling: true}, BaseURL: "http://127.0.0.1:11434/v1", Model: "llama3", ToolUseMode: providers.ToolUseNative, Configured: true, Active: true, Implemented: true},
			{ID: "openai", CatalogID: "openai", Name: "OpenAI", Type: providers.TypeOpenAICompat, Configured: true, Implemented: true},
		},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/provider local-ai")
	if err != nil {
		t.Fatalf("Handle(custom provider) error = %v", err)
	}
	if result.Form == nil || customFormFieldValue(t, result.Form, "name") != "Local AI" || customFormFieldValue(t, result.Form, "key") != "Required" {
		t.Fatalf("custom provider form = %#v", result.Form)
	}
	editForm := result.Form

	result, err = d.Handle(context.Background(), "local", "/provider use local-ai")
	if err != nil {
		t.Fatalf("Handle(provider use) error = %v", err)
	}
	if rt.updatedProvider != "local-ai" || !result.ReloadSnapshot {
		t.Fatalf("updatedProvider=%q reload=%v, want local-ai/reload", rt.updatedProvider, result.ReloadSnapshot)
	}

	if customFormFieldValue(t, editForm, "tools") != "Enabled" {
		t.Fatalf("edit tool mode field = %#v, want Enabled", editForm.Fields)
	}

	result, err = d.Handle(context.Background(), "local", customFormFieldCommand(t, editForm, "tools"))
	if err != nil {
		t.Fatalf("Handle(custom edit tools field) error = %v", err)
	}
	if result.Picker == nil || result.Picker.HideBackItem != true {
		t.Fatalf("edit tool mode picker = %#v, want picker without back row", result.Picker)
	}

	result, err = d.Handle(context.Background(), "local", pickerItemCommand(t, result.Picker, "disabled"))
	if err != nil {
		t.Fatalf("Handle(custom edit tools) error = %v", err)
	}
	if result.Form == nil || customFormFieldValue(t, result.Form, "tools") != "Disabled" {
		t.Fatalf("edit form after tools = %#v", result.Form)
	}

	result, err = d.Handle(context.Background(), "local", result.Form.SubmitCommand)
	if err != nil {
		t.Fatalf("Handle(custom edit save) error = %v", err)
	}
	if rt.configuredProvider != "local-ai" || rt.configuredUpdate.Name != "Local AI" || rt.configuredUpdate.Model != "llama3" || rt.configuredUpdate.APIKey != "" || rt.configuredUpdate.ToolUseMode != providers.ToolUseDisabled || rt.configuredUpdate.Active {
		t.Fatalf("configured edit = provider %q update %#v", rt.configuredProvider, rt.configuredUpdate)
	}

	result, err = d.Handle(context.Background(), "local", "/provider custom delete local-ai")
	if err != nil {
		t.Fatalf("Handle(custom delete) error = %v", err)
	}
	if result.Confirm == nil || result.Confirm.ConfirmCommand != "/provider custom delete-confirm local-ai" {
		t.Fatalf("delete confirm = %#v", result.Confirm)
	}

	result, err = d.Handle(context.Background(), "local", "/provider custom delete-confirm local-ai")
	if err != nil {
		t.Fatalf("Handle(custom delete confirm) error = %v", err)
	}
	if rt.deletedProvider != "local-ai" || !result.ReloadSnapshot {
		t.Fatalf("deletedProvider=%q reload=%v, want local-ai/reload", rt.deletedProvider, result.ReloadSnapshot)
	}
}

func TestDispatcherStatusAndRestart(t *testing.T) {
	rt := &fakeRuntime{}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/status")
	if err != nil {
		t.Fatalf("Handle(/status) error = %v", err)
	}
	if result.Info == nil {
		t.Fatal("expected status info rows")
	}
	if got := result.Info.Rows; len(got) != 3 || got[0].Label != "Daemon" || got[1].Label != "Sys RAM" || got[2].Label != "CPU" {
		t.Fatalf("status rows = %#v", got)
	}
	if !strings.Contains(result.Text, "Daemon  26.0 MiB") || !strings.Contains(result.Text, "Sys RAM 512.0 MiB / 512.0 MiB") || !strings.Contains(result.Text, "CPU     21% / 79%") {
		t.Fatalf("status text = %q", result.Text)
	}

	result, err = d.Handle(context.Background(), "local", "/restart")
	if err != nil {
		t.Fatalf("Handle(/restart) error = %v", err)
	}
	if result.Confirm == nil || result.Confirm.ConfirmCommand != "/restart confirm" {
		t.Fatalf("restart confirm = %#v", result.Confirm)
	}
	if rt.restarted {
		t.Fatal("restart should wait for confirmation")
	}

	result, err = d.Handle(context.Background(), "local", "/restart confirm")
	if err != nil {
		t.Fatalf("Handle(/restart confirm) error = %v", err)
	}
	if !rt.restarted {
		t.Fatal("expected daemon restart")
	}
	if result.Text != "Server daemon restart requested." {
		t.Fatalf("restart text = %q", result.Text)
	}
}

func TestDispatcherContextCompactRequiresConfirm(t *testing.T) {
	rt := &fakeRuntime{
		binding:  core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{{ID: "session_1", Title: "Docs"}},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/context compact")
	if err != nil {
		t.Fatalf("Handle(/context compact) error = %v", err)
	}
	if result.Confirm == nil || result.Confirm.ConfirmCommand != "/context compact confirm" {
		t.Fatalf("compact confirm = %#v", result.Confirm)
	}
	if rt.compacted {
		t.Fatal("compact should wait for confirmation")
	}

	result, err = d.Handle(context.Background(), "local", "/context compact confirm")
	if err != nil {
		t.Fatalf("Handle(/context compact confirm) error = %v", err)
	}
	if !rt.compacted {
		t.Fatal("expected compact to run after confirmation")
	}
	if !strings.Contains(result.Text, "Context compacted") || !result.ReloadSnapshot {
		t.Fatalf("compact result = %#v", result)
	}
}

func TestDispatcherContextPickerUsesExplicitCommands(t *testing.T) {
	rt := &fakeRuntime{
		binding:  core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{{ID: "session_1", Title: "Docs"}},
	}
	d := New(rt, "/tmp")

	result, err := d.Handle(context.Background(), "local", "/context")
	if err != nil {
		t.Fatalf("Handle(/context) error = %v", err)
	}
	if result.Picker == nil {
		t.Fatal("expected context picker")
	}
	items := PresentPickerItems(*result.Picker)
	if got := items[0].Command; got != "/context info" {
		t.Fatalf("context info command = %q, want /context info", got)
	}
	if got := items[1].Command; got != "/context compact" {
		t.Fatalf("context compact command = %q, want /context compact", got)
	}
	if got := items[2].Command; got != "" {
		t.Fatalf("context close command = %q, want empty", got)
	}
}

func TestDispatcherDeleteCurrentSessionRebindsFallback(t *testing.T) {
	rt := &fakeRuntime{
		binding: core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{
			{ID: "session_1", Title: "Docs"},
			{ID: "session_2", Title: "Ops"},
		},
	}
	d := New(rt, "/tmp")
	d.now = func() time.Time { return time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC) }

	result, err := d.Handle(context.Background(), "local", "/session delete-confirmed session_1")
	if err != nil {
		t.Fatalf("Handle(delete-confirmed) error = %v", err)
	}
	if !result.ReloadSnapshot {
		t.Fatal("expected snapshot reload")
	}
	if rt.binding.SessionID != "session_2" {
		t.Fatalf("binding.SessionID = %q, want session_2", rt.binding.SessionID)
	}
	if len(rt.sessions) != 1 || rt.sessions[0].ID != "session_2" {
		t.Fatalf("sessions = %#v", rt.sessions)
	}
}

func TestDispatcherDeleteLastSessionCreatesReplacement(t *testing.T) {
	rt := &fakeRuntime{
		binding: core.ClientBinding{SessionID: "session_1"},
		sessions: []core.Session{
			{ID: "session_1", Title: "Docs"},
		},
	}
	d := New(rt, "/tmp")
	d.now = func() time.Time { return time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC) }

	result, err := d.Handle(context.Background(), "local", "/session delete-confirmed session_1")
	if err != nil {
		t.Fatalf("Handle(delete-confirmed last) error = %v", err)
	}
	if !result.ReloadSnapshot {
		t.Fatal("expected snapshot reload")
	}
	if rt.created != 1 || rt.binding.SessionID != "session_new" {
		t.Fatalf("created=%d binding=%q", rt.created, rt.binding.SessionID)
	}
	if len(rt.sessions) != 1 || rt.sessions[0].Title != "Local chat 2026-04-23 17:00" {
		t.Fatalf("replacement session = %#v", rt.sessions)
	}
}
