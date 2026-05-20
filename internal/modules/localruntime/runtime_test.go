package localruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestRuntimeDirUsesInstallerOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", tmp)

	provider := setup.VoiceProviderOption{
		ID:   "piper",
		Name: "Piper",
		Config: setup.VoiceProviderConfig{
			BinaryPath: "piper",
		},
	}

	binary := filepath.Join(tmp, "piper-venv", "bin", "piper")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := New("").VoiceBinaryPath(provider)
	if err != nil {
		t.Fatalf("VoiceBinaryPath() error = %v", err)
	}
	if got != binary {
		t.Fatalf("VoiceBinaryPath() = %q, want %q", got, binary)
	}
}

func TestWhisperCLISupportsOnlyWaveInput(t *testing.T) {
	wav := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	if !whisperCLISupportsAudioFile("voice.wav", wav) {
		t.Fatal("whisperCLISupportsAudioFile() rejected WAV input")
	}
	if whisperCLISupportsAudioFile("voice.ogg", []byte("OggS")) {
		t.Fatal("whisperCLISupportsAudioFile() accepted OGG input")
	}
	if whisperCLISupportsAudioFile("voice.mp3", []byte("ID3")) {
		t.Fatal("whisperCLISupportsAudioFile() accepted MP3 input")
	}
	if whisperCLISupportsAudioFile("voice.wav", []byte("not a wave")) {
		t.Fatal("whisperCLISupportsAudioFile() accepted invalid WAV input")
	}
}

func TestPiperPersistentTextToSpeechUsesRunningProcess(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "local")
	runtimeDir := filepath.Join(tmp, "runtime")
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", runtimeDir)

	voiceID := "en_US-test-medium"
	modelPath := filepath.Join(root, "voice", "tts", "piper", voiceID, voiceID+".onnx")
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath+".json", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(tmp, "fake-piper")
	script := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir)
      out="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
mkdir -p "$out"
i=0
while IFS= read -r line; do
  i=$((i + 1))
  printf 'WAVE:%s\n' "$line" > "$out/out-$i.wav"
done
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	provider := setup.VoiceProviderOption{
		ID:         "piper",
		Name:       "Piper",
		Local:      true,
		Downloaded: true,
		Config: setup.VoiceProviderConfig{
			VoiceID:     voiceID,
			RuntimeMode: "always_running",
			BinaryPath:  binary,
		},
	}
	runtime := New(root)
	defer runtime.stopPiperProcess(provider)

	content, err := runtime.PiperTextToSpeech(context.Background(), provider, "hello from persistent piper")
	if err != nil {
		t.Fatalf("PiperTextToSpeech() error = %v", err)
	}
	if !strings.Contains(string(content), "hello from persistent piper") {
		t.Fatalf("PiperTextToSpeech() content = %q", string(content))
	}
	if !runtime.piperProcessRunning(provider) {
		t.Fatal("expected persistent Piper process to remain running")
	}
	if files := piperOutputFiles(runtime.piperOutputDir(provider)); len(files) != 0 {
		t.Fatalf("persistent Piper output files = %#v, want cleaned up", files)
	}
}

func TestDecorateVoiceProviderKeepsInstalledConfiguredPiperVoiceOutsideCatalog(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "local")
	voiceID := "de_DE-thorsten-medium"
	runtime := New(root)
	modelPath := runtime.VoiceModelPathForID(setup.VoiceModuleTTS, setup.VoiceProviderOption{ID: "piper"}, voiceID)
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}

	piperCatalogCache.Lock()
	oldExpires := piperCatalogCache.expires
	oldFailUntil := piperCatalogCache.failUntil
	oldModels := append([]setup.VoiceModelOption(nil), piperCatalogCache.models...)
	piperCatalogCache.expires = time.Now().Add(time.Hour)
	piperCatalogCache.failUntil = time.Time{}
	piperCatalogCache.models = []setup.VoiceModelOption{{ID: "en_US-lessac-medium", Name: "Lessac Medium"}}
	piperCatalogCache.Unlock()
	t.Cleanup(func() {
		piperCatalogCache.Lock()
		piperCatalogCache.expires = oldExpires
		piperCatalogCache.failUntil = oldFailUntil
		piperCatalogCache.models = oldModels
		piperCatalogCache.Unlock()
	})

	provider := setup.VoiceProviderOption{
		ID:    "piper",
		Name:  "Piper",
		Local: true,
		Config: setup.VoiceProviderConfig{
			VoiceID:     voiceID,
			RuntimeMode: "per_task",
			BinaryPath:  "piper",
		},
		Models: []setup.VoiceModelOption{{ID: "ru_RU-ruslan-medium", Name: "Ruslan Medium"}},
	}

	got := runtime.DecorateVoiceProvider(setup.VoiceModuleTTS, provider)
	if !got.Downloaded {
		t.Fatal("DecorateVoiceProvider().Downloaded = false, want true")
	}
	var installed setup.VoiceModelOption
	for _, model := range got.Models {
		if model.ID == voiceID {
			installed = model
			break
		}
	}
	if installed.ID == "" {
		t.Fatalf("DecorateVoiceProvider().Models missing configured voice %q", voiceID)
	}
	if !installed.Installed {
		t.Fatalf("configured voice Installed = false, want true")
	}
	if installed.Path != modelPath {
		t.Fatalf("configured voice Path = %q, want %q", installed.Path, modelPath)
	}
}
