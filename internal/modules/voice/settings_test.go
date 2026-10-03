package voice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// voiceDaemon is a voice module on an offline runtime in a temporary
// directory, applied from cfg after every change as the daemon does.
type voiceDaemon struct {
	t      *testing.T
	root   string
	module *Module
	cfg    setup.Config
}

func newVoiceDaemon(t *testing.T, moduleID string, cfg setup.Config) *voiceDaemon {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	runtime := localruntime.New(root)
	runtime.Offline = true
	d := &voiceDaemon{t: t, root: root, module: New(moduleID, runtime), cfg: cfg}
	d.apply()
	return d
}

func (d *voiceDaemon) apply() {
	d.t.Helper()
	if err := d.module.Apply(context.Background(), d.cfg); err != nil {
		d.t.Fatal(err)
	}
}

func (d *voiceDaemon) change(value string, path ...string) modules.Change {
	d.t.Helper()
	change, err := d.module.Change(context.Background(), path, value)
	if err != nil {
		d.t.Fatalf("%v = %q: %v", path, value, err)
	}
	if change.Config != nil {
		if err := change.Config(&d.cfg); err != nil {
			d.t.Fatal(err)
		}
		d.apply()
	}
	return change
}

func (d *voiceDaemon) item(path ...string) modules.Item {
	d.t.Helper()
	item, ok := modules.Find(d.module.Settings(context.Background()), path)
	if !ok {
		d.t.Fatalf("no item %v", path)
	}
	return item
}

// install puts files where the runtime looks for them.
func (d *voiceDaemon) install(paths ...string) {
	d.t.Helper()
	for _, path := range paths {
		path = filepath.Join(filepath.Dir(d.root), path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			d.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
			d.t.Fatal(err)
		}
	}
}

func (d *voiceDaemon) installWhisper(models ...string) {
	d.install("runtime/whisper.cpp/build/bin/whisper-cli", "runtime/whisper.cpp/build/bin/whisper-server")
	for _, model := range models {
		d.install("state/voice/stt/whispercpp/" + model + "/ggml-" + model + ".bin")
	}
}

func keys(items []modules.Item) []string {
	out := []string{}
	for _, item := range items {
		out = append(out, item.Key)
	}
	return out
}

func TestTextToSpeechScreenAsksBeforeDownloadingAnEngine(t *testing.T) {
	d := newVoiceDaemon(t, setup.VoiceModuleTTS, setup.Config{})

	provider := d.item("provider")
	if provider.Value != "off" || len(provider.Options) != 3 || provider.Options[1].Confirm != "Download the Piper engine?" {
		t.Fatalf("provider = %+v", provider)
	}
	if got := keys(d.item("piper").Items); !slices.Equal(got, []string{"engine", "voices", "run_mode", "status"}) {
		t.Fatalf("piper page = %v", got)
	}
	if got := keys(d.item("supertonic").Items); !slices.Equal(got, []string{"engine", "voice", "language", "threads", "run_mode", "status"}) {
		t.Fatalf("supertonic page = %v", got)
	}
	add := d.item("piper", "voices", "add")
	if len(add.Options) != 2 || add.Options[0].Group != "English" || add.Options[1].Group != "Russian" {
		t.Fatalf("voices to add = %+v", add.Options)
	}
}

func TestChoosingWhisperWithoutAModelOpensTheModels(t *testing.T) {
	d := newVoiceDaemon(t, setup.VoiceModuleSTT, setup.Config{})
	d.installWhisper()

	change := d.change("whispercpp", "provider")

	if change.Config != nil || !slices.Equal(change.Open, []string{"whispercpp", "models", "add"}) || change.Message == "" {
		t.Fatalf("change = %+v", change)
	}
	d.installWhisper("small")
	d.change("whispercpp", "provider")
	if stt := d.cfg.Modules.SpeechToText; !stt.Enabled || stt.ProviderID != "whispercpp" || stt.Providers["whispercpp"].ModelID != "small" {
		t.Fatalf("stt = %+v", stt)
	}
}

func TestDeletingTheActiveModelSelectsAnotherOrTurnsTheModuleOff(t *testing.T) {
	d := newVoiceDaemon(t, setup.VoiceModuleSTT, setup.Config{Modules: setup.ModulesConfig{SpeechToText: setup.VoiceModuleConfig{
		Enabled: true, ProviderID: "whispercpp", Providers: map[string]setup.VoiceProviderConfig{"whispercpp": {ModelID: "tiny"}},
	}}})
	d.installWhisper("tiny", "base")

	if got := keys(d.item("whispercpp", "models").Items); !slices.Equal(got, []string{"tiny", "base", "add"}) {
		t.Fatalf("models = %v", got)
	}
	if d.item("whispercpp", "models", "tiny").Display != "Active · ~39 MB" {
		t.Fatalf("tiny = %+v", d.item("whispercpp", "models", "tiny"))
	}
	d.change("", "whispercpp", "models", "base", "use")
	if model := d.cfg.Modules.SpeechToText.Providers["whispercpp"].ModelID; model != "base" {
		t.Fatalf("model after use = %q", model)
	}

	change := d.change("", "whispercpp", "models", "base", "delete")
	if model := d.cfg.Modules.SpeechToText.Providers["whispercpp"].ModelID; model != "tiny" || !slices.Equal(change.Open, []string{"whispercpp", "models"}) {
		t.Fatalf("after deleting the active model: model %q, change %+v", model, change)
	}
	d.change("", "whispercpp", "models", "tiny", "delete")
	if d.cfg.Modules.SpeechToText.Enabled {
		t.Fatal("the module stayed on without a model")
	}
	if _, err := d.module.Change(context.Background(), []string{"whispercpp", "models", "tiny", "use"}, ""); !errors.Is(err, modules.ErrInvalidSetting) {
		t.Fatalf("using a deleted model = %v", err)
	}
}

func TestProviderSettingsAreChecked(t *testing.T) {
	d := newVoiceDaemon(t, setup.VoiceModuleSTT, setup.Config{})

	d.change("ru", "whispercpp", "language")
	d.change("4", "whispercpp", "threads")
	d.change("always_running", "whispercpp", "run_mode")
	if got := d.cfg.Modules.SpeechToText.Providers["whispercpp"]; got.Language != "ru" || got.Threads != 4 || got.RuntimeMode != "always_running" {
		t.Fatalf("whisper = %+v", got)
	}
	for _, bad := range []struct{ key, value string }{{"language", "klingon"}, {"threads", "many"}, {"run_mode", "sometimes"}} {
		if _, err := d.module.Change(context.Background(), []string{"whispercpp", bad.key}, bad.value); !errors.Is(err, modules.ErrInvalidSetting) {
			t.Fatalf("%s = %q: %v", bad.key, bad.value, err)
		}
	}
	if _, err := d.module.Change(context.Background(), []string{"piper", "language"}, "en"); !errors.Is(err, modules.ErrUnknownSetting) {
		t.Fatalf("an STT module's piper = %v", err)
	}
}
