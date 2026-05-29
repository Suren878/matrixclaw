package localruntime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestDecorateVoiceProviderLocalRuntimeFields(t *testing.T) {
	blockVoiceCatalogFetches(t)

	tests := []struct {
		name           string
		moduleID       string
		provider       setup.VoiceProviderOption
		setupFiles     func(t *testing.T, r *Runtime)
		wantDownloaded bool
		wantRuntime    bool
		wantState      string
		wantStatus     string
		wantDetail     string
		wantModelPath  bool
		wantEndpoint   bool
	}{
		{
			name:     "piper installed voice and runtime",
			moduleID: setup.VoiceModuleTTS,
			provider: setup.VoiceProviderOption{
				ID: "piper", Name: "Piper", Local: true,
				Config: setup.VoiceProviderConfig{VoiceID: "en_US-lessac-medium", RuntimeMode: "per_task", BinaryPath: "piper"},
				Models: []setup.VoiceModelOption{{ID: "en_US-lessac-medium", Name: "Lessac Medium"}},
			},
			setupFiles: func(t *testing.T, r *Runtime) {
				writeExecutable(t, r.managedPiperBinaryPath())
				path := r.VoiceModelPath(setup.VoiceModuleTTS, setup.VoiceProviderOption{ID: "piper", Config: setup.VoiceProviderConfig{VoiceID: "en_US-lessac-medium"}})
				writeFile(t, path, "model")
				writeFile(t, path+".json", "{}")
			},
			wantDownloaded: true,
			wantRuntime:    true,
			wantState:      RuntimeStopped,
			wantStatus:     "Local · run per task",
			wantModelPath:  true,
			wantEndpoint:   true,
		},
		{
			name:     "supertonic installed shared runtime",
			moduleID: setup.VoiceModuleTTS,
			provider: setup.VoiceProviderOption{
				ID: "supertonic", Name: "Supertonic 3", Local: true,
				Config: setup.VoiceProviderConfig{VoiceID: "M1", RuntimeMode: "per_task", BinaryPath: "supertonic"},
			},
			setupFiles: func(t *testing.T, r *Runtime) {
				writeExecutable(t, r.managedSupertonicBinaryPath())
				writeSupertonicCache(t, r.supertonicModelCacheDir())
			},
			wantDownloaded: true,
			wantRuntime:    true,
			wantState:      RuntimeStopped,
			wantStatus:     "Local · run per task",
		},
		{
			name:     "whispercpp installed model and runtime",
			moduleID: setup.VoiceModuleSTT,
			provider: setup.VoiceProviderOption{
				ID: "whispercpp", Name: "Whisper.cpp", Local: true,
				Config: setup.VoiceProviderConfig{ModelID: "base", RuntimeMode: "per_task", BinaryPath: "whisper-cli"},
				Models: []setup.VoiceModelOption{{ID: "base", Name: "Base"}},
			},
			setupFiles: func(t *testing.T, r *Runtime) {
				writeExecutable(t, r.managedWhisperCLIPath())
				writeExecutable(t, r.managedWhisperServerPath())
				path := r.VoiceModelPath(setup.VoiceModuleSTT, setup.VoiceProviderOption{ID: "whispercpp", Config: setup.VoiceProviderConfig{ModelID: "base"}})
				writeFile(t, path, "model")
			},
			wantDownloaded: true,
			wantRuntime:    true,
			wantState:      RuntimeStopped,
			wantStatus:     "Local · run per task",
			wantModelPath:  true,
			wantEndpoint:   true,
		},
		{
			name:     "piper missing local files",
			moduleID: setup.VoiceModuleTTS,
			provider: setup.VoiceProviderOption{
				ID: "piper", Name: "Piper", Local: true,
				Config: setup.VoiceProviderConfig{VoiceID: "en_US-lessac-medium", RuntimeMode: "per_task", BinaryPath: "piper"},
				Models: []setup.VoiceModelOption{{ID: "en_US-lessac-medium", Name: "Lessac Medium"}},
			},
			wantState:     RuntimeUnavailable,
			wantStatus:    "Local · not installed",
			wantDetail:    "Download the selected local files before local voice can run",
			wantModelPath: true,
			wantEndpoint:  true,
		},
		{
			name:     "supertonic missing shared runtime",
			moduleID: setup.VoiceModuleTTS,
			provider: setup.VoiceProviderOption{
				ID: "supertonic", Name: "Supertonic 3", Local: true,
				Config: setup.VoiceProviderConfig{VoiceID: "M1", RuntimeMode: "per_task", BinaryPath: "supertonic"},
			},
			wantState:  RuntimeUnavailable,
			wantStatus: "Local · runtime missing",
			wantDetail: "Supertonic 3 runtime is not installed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(t.TempDir())
			t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
			t.Setenv("SUPERTONIC_CACHE_DIR", filepath.Join(t.TempDir(), "supertonic-cache"))
			if tt.setupFiles != nil {
				tt.setupFiles(t, r)
			}

			got := r.DecorateVoiceProvider(tt.moduleID, tt.provider)

			if got.Downloaded != tt.wantDownloaded {
				t.Fatalf("Downloaded = %v, want %v", got.Downloaded, tt.wantDownloaded)
			}
			if got.RuntimeInstalled != tt.wantRuntime {
				t.Fatalf("RuntimeInstalled = %v, want %v", got.RuntimeInstalled, tt.wantRuntime)
			}
			if got.RuntimeState != tt.wantState {
				t.Fatalf("RuntimeState = %q, want %q", got.RuntimeState, tt.wantState)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.RuntimeDetail != tt.wantDetail {
				t.Fatalf("RuntimeDetail = %q, want %q", got.RuntimeDetail, tt.wantDetail)
			}
			if (strings.TrimSpace(got.ModelPath) != "") != tt.wantModelPath {
				t.Fatalf("ModelPath presence = %v, want %v (%q)", strings.TrimSpace(got.ModelPath) != "", tt.wantModelPath, got.ModelPath)
			}
			if (strings.TrimSpace(got.Endpoint) != "") != tt.wantEndpoint {
				t.Fatalf("Endpoint presence = %v, want %v (%q)", strings.TrimSpace(got.Endpoint) != "", tt.wantEndpoint, got.Endpoint)
			}
			if tt.wantRuntime && strings.TrimSpace(got.RuntimePath) == "" {
				t.Fatal("RuntimePath is empty for installed runtime")
			}
		})
	}
}

func TestDecorateVoiceProviderActionIDs(t *testing.T) {
	blockVoiceCatalogFetches(t)

	r := New(t.TempDir())
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))

	tests := []struct {
		name     string
		moduleID string
		provider setup.VoiceProviderOption
		want     setup.VoiceProviderActionIDs
	}{
		{
			name:     "piper",
			moduleID: setup.VoiceModuleTTS,
			provider: setup.VoiceProviderOption{ID: "piper", Name: "Piper", Local: true},
			want: setup.VoiceProviderActionIDs{
				InstallRuntime:           ActionInstallRuntime,
				DeleteRuntime:            ActionDeleteRuntime,
				DownloadModel:            ActionDownload,
				DownloadModelWithRuntime: "download-with-runtime",
				DeleteModel:              ActionDelete,
				Start:                    ActionStart,
				Stop:                     ActionStop,
			},
		},
		{
			name:     "supertonic",
			moduleID: setup.VoiceModuleTTS,
			provider: setup.VoiceProviderOption{ID: "supertonic", Name: "Supertonic 3", Local: true},
			want: setup.VoiceProviderActionIDs{
				InstallRuntime: ActionInstallRuntime,
				DeleteRuntime:  ActionDeleteRuntime,
				Start:          ActionStart,
				Stop:           ActionStop,
			},
		},
		{
			name:     "whispercpp",
			moduleID: setup.VoiceModuleSTT,
			provider: setup.VoiceProviderOption{ID: "whispercpp", Name: "Whisper.cpp", Local: true},
			want: setup.VoiceProviderActionIDs{
				InstallRuntime:           ActionInstallRuntime,
				DeleteRuntime:            ActionDeleteRuntime,
				DownloadModel:            ActionDownload,
				DownloadModelWithRuntime: "download-with-runtime",
				DeleteModel:              ActionDelete,
				Start:                    ActionStart,
				Stop:                     ActionStop,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.DecorateVoiceProvider(tt.moduleID, tt.provider)
			if !reflect.DeepEqual(got.ActionIDs, tt.want) {
				t.Fatalf("ActionIDs = %#v, want %#v", got.ActionIDs, tt.want)
			}
		})
	}
}

func TestDecorateVoiceModulesCopiesSelectedProviderState(t *testing.T) {
	blockVoiceCatalogFetches(t)

	root := t.TempDir()
	r := New(root)
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	provider := setup.VoiceProviderOption{
		ID: "piper", Name: "Piper", Local: true,
		Config: setup.VoiceProviderConfig{VoiceID: "en_US-lessac-medium", RuntimeMode: "per_task", BinaryPath: "piper"},
		Models: []setup.VoiceModelOption{{ID: "en_US-lessac-medium", Name: "Lessac Medium"}},
	}
	path := r.VoiceModelPath(setup.VoiceModuleTTS, provider)
	writeFile(t, path, "model")
	writeFile(t, path+".json", "{}")
	writeExecutable(t, r.managedPiperBinaryPath())

	modules := []setup.VoiceModuleDescriptor{{
		ID: "tts", Title: "Text to Speech", Enabled: true, ProviderID: "piper",
		Providers: []setup.VoiceProviderOption{provider},
	}}

	got := r.DecorateVoiceModules(modules)
	if len(got) != 1 {
		t.Fatalf("len(DecorateVoiceModules) = %d, want 1", len(got))
	}
	if got[0].Status != "Local · run per task" {
		t.Fatalf("module Status = %q, want Local · run per task", got[0].Status)
	}
	if !got[0].Local || !reflect.DeepEqual(got[0].Config, provider.Config) {
		t.Fatalf("selected provider state not copied to module: %#v", got[0])
	}
}

func TestApplyVoiceActionRejectsUnsupportedTargets(t *testing.T) {
	blockVoiceCatalogFetches(t)

	r := New(t.TempDir())
	remote := setup.VoiceProviderOption{ID: "remote", Name: "Remote"}
	if _, err := r.ApplyVoiceAction(context.Background(), setup.VoiceModuleTTS, remote, setup.VoiceProviderActionRequest{Action: ActionDownload}); err == nil || err.Error() != "voice provider is not local" {
		t.Fatalf("remote provider error = %v, want voice provider is not local", err)
	}

	local := setup.VoiceProviderOption{ID: "custom", Name: "Custom", Local: true}
	if _, err := r.ApplyVoiceAction(context.Background(), setup.VoiceModuleTTS, local, setup.VoiceProviderActionRequest{Action: "bogus"}); err == nil || err.Error() != `unsupported local voice action "bogus"` {
		t.Fatalf("unsupported action error = %v, want unsupported local voice action", err)
	}
	if _, err := r.ApplyVoiceAction(context.Background(), setup.VoiceModuleTTS, local, setup.VoiceProviderActionRequest{Action: ActionDownload}); err == nil || err.Error() != "local voice model path is empty" {
		t.Fatalf("unsupported provider download error = %v, want empty local model path", err)
	}
}

func blockVoiceCatalogFetches(t *testing.T) {
	t.Helper()
	now := time.Now()
	piperCatalogCache.Lock()
	oldPiperExpires := piperCatalogCache.expires
	oldPiperFailUntil := piperCatalogCache.failUntil
	oldPiperModels := append([]setup.VoiceModelOption(nil), piperCatalogCache.models...)
	piperCatalogCache.models = nil
	piperCatalogCache.expires = time.Time{}
	piperCatalogCache.failUntil = now.Add(time.Hour)
	piperCatalogCache.Unlock()

	supertonicCatalogCache.Lock()
	oldSupertonicExpires := supertonicCatalogCache.expires
	oldSupertonicFailUntil := supertonicCatalogCache.failUntil
	oldSupertonicModels := append([]setup.VoiceModelOption(nil), supertonicCatalogCache.models...)
	supertonicCatalogCache.models = nil
	supertonicCatalogCache.expires = time.Time{}
	supertonicCatalogCache.failUntil = now.Add(time.Hour)
	supertonicCatalogCache.Unlock()

	whisperCatalogCache.Lock()
	oldWhisperExpires := whisperCatalogCache.expires
	oldWhisperFailUntil := whisperCatalogCache.failUntil
	oldWhisperModels := append([]setup.VoiceModelOption(nil), whisperCatalogCache.models...)
	whisperCatalogCache.models = nil
	whisperCatalogCache.expires = time.Time{}
	whisperCatalogCache.failUntil = now.Add(time.Hour)
	whisperCatalogCache.Unlock()

	t.Cleanup(func() {
		piperCatalogCache.Lock()
		piperCatalogCache.expires = oldPiperExpires
		piperCatalogCache.failUntil = oldPiperFailUntil
		piperCatalogCache.models = oldPiperModels
		piperCatalogCache.Unlock()
		supertonicCatalogCache.Lock()
		supertonicCatalogCache.expires = oldSupertonicExpires
		supertonicCatalogCache.failUntil = oldSupertonicFailUntil
		supertonicCatalogCache.models = oldSupertonicModels
		supertonicCatalogCache.Unlock()
		whisperCatalogCache.Lock()
		whisperCatalogCache.expires = oldWhisperExpires
		whisperCatalogCache.failUntil = oldWhisperFailUntil
		whisperCatalogCache.models = oldWhisperModels
		whisperCatalogCache.Unlock()
	})
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	writeFile(t, path, "#!/bin/sh\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeSupertonicCache(t *testing.T, dir string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "voice_styles", "M1.json"),
		filepath.Join(dir, "onnx", "tts.json"),
		filepath.Join(dir, "onnx", "text_encoder.onnx"),
		filepath.Join(dir, "onnx", "duration_predictor.onnx"),
		filepath.Join(dir, "onnx", "vector_estimator.onnx"),
		filepath.Join(dir, "onnx", "vocoder.onnx"),
	} {
		writeFile(t, path, "ok")
	}
}
