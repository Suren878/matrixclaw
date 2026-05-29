package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
)

func TestRepresentativePickersHaveExplicitCommands(t *testing.T) {
	ctx := context.Background()
	check := func(result Result, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		assertPickerCommands(t, result.Picker)
	}

	sessionRuntime := &sessionModelTestRuntime{sessions: []core.Session{{ID: "s1", Title: "Main", RuntimeID: core.SessionRuntimeMatrixClaw, ModelID: "sonnet"}}}
	sessionDispatcher := New(sessionRuntime, "")
	check(sessionDispatcher.handleSessions(ctx, "terminal"))
	assertPickerCommands(t, sessionDispatcher.sessionMenuPicker(sessionRuntime.sessions[0]))

	externalRuntime := &sessionRuntimePickerTestRuntime{agents: []core.ExternalAgentDescriptor{{ID: "codex-app", DisplayName: "Codex", Installed: true, Enabled: true}}}
	externalDispatcher := New(externalRuntime, "")
	check(externalDispatcher.sessionRuntimePicker(ctx))
	check(externalDispatcher.externalAgentsPicker(ctx))
	check(externalDispatcher.externalAgentPicker(ctx, "codex-app"))

	modulesRuntime := &modulesTestRuntime{
		skills: []skills.Skill{{ID: "deploy", Name: "Deploy", Description: "Deploy helper", TrustState: skills.TrustTrusted, State: skills.StateActive, Enabled: true}},
		mcp:    setup.MCPConfig{Enabled: true, Servers: []setup.MCPServerConfig{{ID: "docs", Enabled: true, Transport: "stdio", Command: "docs-mcp"}}},
	}
	modulesDispatcher := New(modulesRuntime, "")
	check(modulesDispatcher.skillsRootPicker(ctx))
	check(modulesDispatcher.skillsSectionPicker(ctx, "library", ""))
	check(modulesDispatcher.skillPicker(ctx, "library", "deploy"))
	check(modulesDispatcher.sessionSkillsPicker(ctx, "s1"))
	check(modulesDispatcher.sessionSkillPicker(ctx, "s1", "deploy"))
	check(modulesDispatcher.mcpPicker(ctx))
	check(modulesDispatcher.mcpServerPicker(ctx, "docs"))

	voiceProvider := voicePickerProvider("piper", "Piper", true, true, true)
	voiceDispatcher := New(&voiceRuntimeStub{modules: []setup.VoiceModuleDescriptor{voiceModuleForProvider(setup.VoiceModuleTTS, voiceProvider)}}, "")
	check(voiceDispatcher.voiceModulePicker(ctx, setup.VoiceModuleTTS))
	check(voiceDispatcher.voiceModuleProviderSelectPicker(ctx, setup.VoiceModuleTTS))
	check(voiceDispatcher.voiceLocalProviderPicker(ctx, setup.VoiceModuleTTS, "piper"))
	check(voiceDispatcher.voiceLocalProviderModelPicker(ctx, setup.VoiceModuleTTS, "piper"))
	check(voiceDispatcher.voiceInstalledLocalPicker(ctx, setup.VoiceModuleTTS, "piper"))

	storageDispatcher := New(&pickerValidationStorageRuntime{}, "")
	check(storageDispatcher.storagePicker(ctx))
	check(storageDispatcher.storageFilesPicker(ctx))
	check(storageDispatcher.storageFilePicker(ctx, "notes.md"))
	check(storageDispatcher.storageTempPicker(ctx))
	check(storageDispatcher.storageTempFilePicker(ctx, "tmp.md"))
	check(storageDispatcher.storageTempCleanupSettings(ctx))

	automationDispatcher := New(&pickerValidationAutomationRuntime{}, "")
	check(automationDispatcher.tasksPicker(ctx))
	check(automationDispatcher.tasksArchivePicker(ctx))
	check(automationDispatcher.taskActionsPicker(ctx, "job1"))
}

type pickerValidationStorageRuntime struct{}

func (pickerValidationStorageRuntime) ListTemporaryStorageFiles(context.Context, int) (localstorage.TempListResult, error) {
	return localstorage.TempListResult{
		Files: []localstorage.TempEntry{{Path: "tmp.md", Title: "Temp", Size: 12}},
		Settings: localstorage.TempSettings{
			AutoCleanup: true,
			TTLSeconds:  7 * 24 * 3600,
			MaxBytes:    1024 * 1024 * 1024,
			TotalFiles:  1,
			TotalBytes:  12,
		},
	}, nil
}

func (pickerValidationStorageRuntime) PromoteTemporaryStorageFile(context.Context, string, string) (localstorage.Entry, error) {
	return localstorage.Entry{}, nil
}

func (pickerValidationStorageRuntime) DeleteTemporaryStorageFile(context.Context, string) (localstorage.TempEntry, error) {
	return localstorage.TempEntry{}, nil
}

func (pickerValidationStorageRuntime) CleanupTemporaryStorageFiles(context.Context) (localstorage.CleanupResult, error) {
	return localstorage.CleanupResult{}, nil
}

func (pickerValidationStorageRuntime) UpdateTemporaryStorageSettings(context.Context, *bool, int64, float64) (localstorage.TempSettings, error) {
	return localstorage.TempSettings{}, nil
}

func (pickerValidationStorageRuntime) ListStorageFiles(context.Context, localstorage.ListFilter) (localstorage.ListResult, error) {
	return localstorage.ListResult{Files: []localstorage.Entry{{Path: "notes.md", Title: "Notes", Size: 20}}}, nil
}

func (pickerValidationStorageRuntime) ReadStorageFile(context.Context, string) (localstorage.ReadResult, error) {
	return localstorage.ReadResult{File: localstorage.Entry{Path: "notes.md", Title: "Notes", Size: 20}, Content: "notes"}, nil
}

func (pickerValidationStorageRuntime) DeleteStorageFile(context.Context, string) (localstorage.Entry, error) {
	return localstorage.Entry{}, nil
}

type pickerValidationAutomationRuntime struct{}

func (pickerValidationAutomationRuntime) ClientName() string { return "test" }

func (pickerValidationAutomationRuntime) CreateAutomationJob(context.Context, automation.CreateJobInput) (automation.Job, error) {
	return automation.Job{}, nil
}

func (pickerValidationAutomationRuntime) ListAutomationJobs(context.Context) ([]automation.Job, error) {
	now := time.Now()
	closed := now.Add(-time.Hour)
	return []automation.Job{
		{ID: "job1", Title: "Active task", Status: automation.JobStatusActive, ScheduleMode: automation.ScheduleModeOnce, NextDueAt: &now},
		{ID: "job2", Title: "Closed task", Status: automation.JobStatusCompleted, ScheduleMode: automation.ScheduleModeOnce, DeletedAt: &closed},
	}, nil
}

func (pickerValidationAutomationRuntime) PauseAutomationJob(context.Context, string) (automation.Job, error) {
	return automation.Job{}, nil
}

func (pickerValidationAutomationRuntime) ResumeAutomationJob(context.Context, string) (automation.Job, error) {
	return automation.Job{}, nil
}

func (pickerValidationAutomationRuntime) CompleteAutomationJob(context.Context, string) (automation.Job, error) {
	return automation.Job{}, nil
}

func (pickerValidationAutomationRuntime) DeleteAutomationJob(context.Context, string) (automation.Job, error) {
	return automation.Job{}, nil
}

func (pickerValidationAutomationRuntime) RunAutomationJobNow(context.Context, string) (automation.Fire, error) {
	return automation.Fire{}, nil
}
