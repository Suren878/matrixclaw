package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

type voiceRuntimeStub struct {
	modules []setup.VoiceModuleDescriptor
	actions []setup.VoiceProviderActionRequest
}

func (r *voiceRuntimeStub) VoiceModules(context.Context) ([]setup.VoiceModuleDescriptor, error) {
	return append([]setup.VoiceModuleDescriptor(nil), r.modules...), nil
}

func (r *voiceRuntimeStub) UpdateVoiceModule(_ context.Context, moduleID string, update setup.VoiceModuleUpdate) ([]setup.VoiceModuleDescriptor, error) {
	for i := range r.modules {
		if r.modules[i].ID != moduleID {
			continue
		}
		if update.Enabled != nil {
			r.modules[i].Enabled = *update.Enabled
		}
		if update.ProviderID != "" {
			r.modules[i].ProviderID = update.ProviderID
		}
		if update.ProviderConfig != nil {
			for j := range r.modules[i].Providers {
				if r.modules[i].Providers[j].ID == r.modules[i].ProviderID {
					r.modules[i].Providers[j].Config = *update.ProviderConfig
					r.modules[i].Config = *update.ProviderConfig
				}
			}
		}
	}
	return r.VoiceModules(context.Background())
}

func (r *voiceRuntimeStub) VoiceProviderAction(_ context.Context, moduleID string, providerID string, request setup.VoiceProviderActionRequest) (setup.VoiceProviderOption, error) {
	r.actions = append(r.actions, request)
	for _, module := range r.modules {
		if module.ID != moduleID {
			continue
		}
		for _, provider := range module.Providers {
			if provider.ID == providerID {
				return provider, nil
			}
		}
	}
	return setup.VoiceProviderOption{}, nil
}

func TestVoiceLocalProviderPickersKeepRuntimeCommands(t *testing.T) {
	tests := []struct {
		name     string
		moduleID string
		provider setup.VoiceProviderOption
		want     []PickerItem
	}{
		{
			name:     "piper",
			moduleID: setup.VoiceModuleTTS,
			provider: voicePickerProvider("piper", "Piper", false, true, true),
			want: []PickerItem{
				{ID: "engine", Command: "/modules tts provider-action piper install-runtime", Info: "Not Installed", Role: PickerItemRoleAction},
				{ID: "voice", Command: "/modules tts provider-installed piper"},
				{ID: "run-mode", Command: "/modules tts provider-run-mode piper", Info: "Run per task"},
			},
		},
		{
			name:     "supertonic",
			moduleID: setup.VoiceModuleTTS,
			provider: voicePickerProvider("supertonic", "Supertonic 3", true, true, true),
			want: []PickerItem{
				{ID: "engine", Command: "/modules tts provider-action supertonic delete-runtime", Info: "Installed", Role: PickerItemRoleDanger},
				{ID: "voice-style", Command: "/modules tts provider-model supertonic"},
				{ID: "language", Command: "/modules tts provider-language supertonic"},
				{ID: "run-mode", Command: "/modules tts provider-run-mode supertonic", Info: "Run per task"},
			},
		},
		{
			name:     "whispercpp",
			moduleID: setup.VoiceModuleSTT,
			provider: voicePickerProvider("whispercpp", "Whisper.cpp", false, false, false),
			want: []PickerItem{
				{ID: "engine", Command: "/modules stt provider-action whispercpp install-runtime", Info: "Not Installed · Builds Locally", Role: PickerItemRoleAction},
				{ID: "model", Command: "/modules stt provider-installed whispercpp"},
				{ID: "run-mode", Command: "/modules stt provider-run-mode whispercpp", Info: "Run per task"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &voiceRuntimeStub{modules: []setup.VoiceModuleDescriptor{voiceModuleForProvider(tt.moduleID, tt.provider)}}
			result, err := New(runtime, "").voiceLocalProviderPicker(context.Background(), tt.moduleID, tt.provider.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Picker == nil {
				t.Fatal("Picker = nil")
			}
			for _, want := range tt.want {
				item := requirePickerItem(t, result.Picker, want.ID)
				if want.Command != "" && item.Command != want.Command {
					t.Fatalf("%s command = %q, want %q", want.ID, item.Command, want.Command)
				}
				if want.Info != "" && item.Info != want.Info {
					t.Fatalf("%s info = %q, want %q", want.ID, item.Info, want.Info)
				}
				if want.Role != "" && item.Role != want.Role {
					t.Fatalf("%s role = %q, want %q", want.ID, item.Role, want.Role)
				}
			}
		})
	}
}

func TestVoiceModelPickerKeepsDownloadCommands(t *testing.T) {
	piper := voicePickerProvider("piper", "Piper", true, true, false)
	whisperMissingRuntime := voicePickerProvider("whispercpp", "Whisper.cpp", false, false, false)
	whisperWithRuntime := voicePickerProvider("whispercpp", "Whisper.cpp", true, true, false)

	tests := []struct {
		name     string
		moduleID string
		provider setup.VoiceProviderOption
		modelID  string
		command  string
	}{
		{"piper download", setup.VoiceModuleTTS, piper, "en_US-lessac-medium", "/modules tts provider-action piper download en_US-lessac-medium"},
		{"whisper download with runtime", setup.VoiceModuleSTT, whisperMissingRuntime, "base", "/modules stt provider-action whispercpp download-with-runtime base"},
		{"whisper download", setup.VoiceModuleSTT, whisperWithRuntime, "base", "/modules stt provider-action whispercpp download base"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &voiceRuntimeStub{modules: []setup.VoiceModuleDescriptor{voiceModuleForProvider(tt.moduleID, tt.provider)}}
			result, err := New(runtime, "").voiceLocalProviderModelPicker(context.Background(), tt.moduleID, tt.provider.ID)
			if err != nil {
				t.Fatal(err)
			}
			item := requirePickerItem(t, result.Picker, tt.modelID)
			if item.Command != tt.command {
				t.Fatalf("model command = %q, want %q", item.Command, tt.command)
			}
		})
	}
}

func TestVoiceInstalledLocalActionPickerKeepsDeleteCommand(t *testing.T) {
	provider := voicePickerProvider("piper", "Piper", true, true, true)
	runtime := &voiceRuntimeStub{modules: []setup.VoiceModuleDescriptor{voiceModuleForProvider(setup.VoiceModuleTTS, provider)}}
	result, err := New(runtime, "").voiceInstalledLocalActionPicker(context.Background(), setup.VoiceModuleTTS, "piper en_US-lessac-medium")
	if err != nil {
		t.Fatal(err)
	}

	use := requirePickerItem(t, result.Picker, "use")
	if use.Command != "/modules tts provider-use piper en_US-lessac-medium" || use.Info != "Already active" {
		t.Fatalf("use item = %#v", use)
	}
	deleteItem := requirePickerItem(t, result.Picker, "delete")
	if deleteItem.Command != "/modules tts provider-action piper delete en_US-lessac-medium" || deleteItem.Role != PickerItemRoleDanger {
		t.Fatalf("delete item = %#v", deleteItem)
	}
}

func TestVoiceLocalProviderPickersUseProviderActionIDs(t *testing.T) {
	actionIDs := setup.VoiceProviderActionIDs{
		InstallRuntime:           "engine-install",
		DeleteRuntime:            "engine-remove",
		DownloadModel:            "grab",
		DownloadModelWithRuntime: "grab-engine",
		DeleteModel:              "purge",
		Start:                    "boot",
		Stop:                     "halt",
	}

	provider := voicePickerProvider("piper", "Piper", false, true, false)
	provider.ActionIDs = actionIDs
	runtime := &voiceRuntimeStub{modules: []setup.VoiceModuleDescriptor{voiceModuleForProvider(setup.VoiceModuleTTS, provider)}}
	dispatcher := New(runtime, "")

	setupResult, err := dispatcher.voiceLocalProviderPicker(context.Background(), setup.VoiceModuleTTS, "piper")
	if err != nil {
		t.Fatal(err)
	}
	engine := requirePickerItem(t, setupResult.Picker, "engine")
	if engine.Command != "/modules tts provider-action piper engine-install" {
		t.Fatalf("engine command = %q, want metadata action id", engine.Command)
	}

	modelResult, err := dispatcher.voiceLocalProviderModelPicker(context.Background(), setup.VoiceModuleTTS, "piper")
	if err != nil {
		t.Fatal(err)
	}
	model := requirePickerItem(t, modelResult.Picker, "en_US-lessac-medium")
	if model.Command != "/modules tts provider-action piper grab en_US-lessac-medium" {
		t.Fatalf("model command = %q, want metadata action id", model.Command)
	}

	provider.Models[0].Installed = true
	runtime.modules[0] = voiceModuleForProvider(setup.VoiceModuleTTS, provider)
	installedResult, err := dispatcher.voiceInstalledLocalActionPicker(context.Background(), setup.VoiceModuleTTS, "piper en_US-lessac-medium")
	if err != nil {
		t.Fatal(err)
	}
	deleteItem := requirePickerItem(t, installedResult.Picker, "delete")
	if deleteItem.Command != "/modules tts provider-action piper purge en_US-lessac-medium" {
		t.Fatalf("delete command = %q, want metadata action id", deleteItem.Command)
	}

	confirmResult, err := dispatcher.voiceLocalProviderAction(context.Background(), setup.VoiceModuleTTS, "piper engine-install")
	if err != nil {
		t.Fatal(err)
	}
	if confirmResult.Confirm == nil {
		t.Fatal("Confirm = nil")
	}
	if confirmResult.Confirm.ConfirmCommand != "/modules tts provider-action piper engine-install-confirm" {
		t.Fatalf("ConfirmCommand = %q, want metadata action id confirm", confirmResult.Confirm.ConfirmCommand)
	}
}

func TestVoiceLocalProviderActionConfirmCommands(t *testing.T) {
	provider := voicePickerProvider("piper", "Piper", true, true, true)
	runtime := &voiceRuntimeStub{modules: []setup.VoiceModuleDescriptor{voiceModuleForProvider(setup.VoiceModuleTTS, provider)}}
	dispatcher := New(runtime, "")

	tests := []struct {
		args       string
		confirm    string
		cancel     string
		label      string
		wantDanger bool
	}{
		{"piper install-runtime", "/modules tts provider-action piper install-runtime-confirm", "/modules tts provider-setup piper", "Download", false},
		{"piper delete-runtime", "/modules tts provider-action piper delete-runtime-confirm", "/modules tts provider-setup piper", "Delete", true},
		{"piper start", "/modules tts provider-action piper start-confirm", "/modules tts provider-setup piper", "Start", false},
		{"piper stop", "/modules tts provider-action piper stop-confirm", "/modules tts provider-setup piper", "Stop", true},
		{"piper delete en_US-lessac-medium", "/modules tts provider-action piper delete-confirm en_US-lessac-medium", "/modules tts provider-installed piper", "Delete", true},
	}

	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			result, err := dispatcher.voiceLocalProviderAction(context.Background(), setup.VoiceModuleTTS, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if result.Confirm == nil {
				t.Fatal("Confirm = nil")
			}
			if result.Confirm.ConfirmCommand != tt.confirm {
				t.Fatalf("ConfirmCommand = %q, want %q", result.Confirm.ConfirmCommand, tt.confirm)
			}
			if result.Confirm.CancelCommand != tt.cancel {
				t.Fatalf("CancelCommand = %q, want %q", result.Confirm.CancelCommand, tt.cancel)
			}
			if result.Confirm.ConfirmLabel != tt.label {
				t.Fatalf("ConfirmLabel = %q, want %q", result.Confirm.ConfirmLabel, tt.label)
			}
			if result.Confirm.ConfirmDanger != tt.wantDanger {
				t.Fatalf("ConfirmDanger = %v, want %v", result.Confirm.ConfirmDanger, tt.wantDanger)
			}
		})
	}
}

func requirePickerItem(t *testing.T, picker *PickerData, id string) PickerItem {
	t.Helper()
	if picker == nil {
		t.Fatal("Picker = nil")
		return PickerItem{}
	}
	for _, item := range picker.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("picker item %q not found in %#v", id, picker.Items)
	return PickerItem{}
}

func voiceModuleForProvider(moduleID string, provider setup.VoiceProviderOption) setup.VoiceModuleDescriptor {
	title := "Text to Speech"
	if moduleID == setup.VoiceModuleSTT {
		title = "Speech to Text"
	}
	return setup.VoiceModuleDescriptor{
		ID: moduleID, Title: title, Enabled: true,
		ProviderID: provider.ID, ProviderName: provider.Name,
		Local: provider.Local, Status: provider.Status, Config: provider.Config,
		Providers: []setup.VoiceProviderOption{provider},
	}
}

func voicePickerProvider(id string, name string, runtimeInstalled bool, downloaded bool, modelInstalled bool) setup.VoiceProviderOption {
	provider := setup.VoiceProviderOption{
		ID: id, Name: name, Local: true,
		RuntimeInstalled: runtimeInstalled,
		Downloaded:       downloaded,
		RuntimeState:     "stopped",
		ActionIDs: setup.VoiceProviderActionIDs{
			InstallRuntime:           "install-runtime",
			DeleteRuntime:            "delete-runtime",
			DownloadModel:            "download",
			DownloadModelWithRuntime: "download-with-runtime",
			DeleteModel:              "delete",
			Start:                    "start",
			Stop:                     "stop",
		},
		Config: setup.VoiceProviderConfig{
			VoiceID:     "en_US-lessac-medium",
			ModelID:     "base",
			Language:    "en_US",
			RuntimeMode: "per_task",
		},
	}
	switch id {
	case "supertonic":
		provider.Config.VoiceID = "M1"
		provider.Config.Language = "auto"
		provider.Models = []setup.VoiceModelOption{{ID: "M1", Name: "M1", Installed: modelInstalled}}
	case "whispercpp":
		provider.Models = []setup.VoiceModelOption{{ID: "base", Name: "Base", Installed: modelInstalled}}
	default:
		provider.Models = []setup.VoiceModelOption{{ID: "en_US-lessac-medium", Name: "Lessac Medium", LanguageCode: "en_US", Installed: modelInstalled}}
	}
	if downloaded {
		provider.Status = "Local · run per task"
	} else {
		provider.Status = "Local · not installed"
	}
	return provider
}
