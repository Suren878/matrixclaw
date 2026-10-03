package localruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const fakePiper = `#!/bin/sh
while [ $# -gt 0 ]; do
  if [ "$1" = "--output-dir" ]; then out="$2"; fi
  shift
done
n=0
while read -r line; do
  n=$((n+1))
  printf 'RIFF%s' "$line" > "$out/$n.wav"
done
`

func TestPiperServerSynthesizesAndStopsOnClose(t *testing.T) {
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	r := New(t.TempDir())
	binary := filepath.Join(t.TempDir(), "piper")
	if err := os.WriteFile(binary, []byte(fakePiper), 0o755); err != nil {
		t.Fatal(err)
	}
	provider := VoiceProvider{ID: "piper", Name: "Piper", Local: true, Config: setup.VoiceProviderConfig{
		VoiceID: "en_US-test-medium", BinaryPath: binary, RuntimeMode: "always_running",
	}}
	model := r.VoiceModelPath(setup.VoiceModuleTTS, provider)
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{model, model + ".json"} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, text := range []string{"one", "two"} {
		audio, err := r.piperPersistentTextToSpeech(context.Background(), provider, text)
		if err != nil {
			t.Fatal(err)
		}
		if string(audio) != "RIFF"+text {
			t.Fatalf("audio = %q", audio)
		}
	}
	process, ok := r.procs.Running("piper")
	if !ok {
		t.Fatal("piper server is not running")
	}
	if !r.voiceRuntimeRunning(provider) {
		t.Fatal("runtime does not report piper as running")
	}
	_ = r.Close()
	if process.Running() {
		t.Fatal("Close left the piper server running")
	}
}
