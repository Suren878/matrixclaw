package voice

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestTextToSpeechToolFollowsTheModule(t *testing.T) {
	runtime := localruntime.New(t.TempDir())
	tts := New(setup.VoiceModuleTTS, runtime)
	stt := New(setup.VoiceModuleSTT, runtime)
	on := setup.Config{Modules: setup.ModulesConfig{
		TextToSpeech: setup.VoiceModuleConfig{Enabled: true},
		SpeechToText: setup.VoiceModuleConfig{Enabled: true},
	}}
	for _, module := range []*Module{tts, stt} {
		if err := module.Apply(context.Background(), on); err != nil {
			t.Fatal(err)
		}
	}
	if tools := tts.Tools(); len(tools) != 1 || tools[0].Spec().ID != TextToSpeechToolID {
		t.Fatalf("tts tools = %v", tools)
	}
	if tools := stt.Tools(); len(tools) != 0 {
		t.Fatalf("stt tools = %v", tools)
	}
	if err := tts.Apply(context.Background(), setup.Config{}); err != nil {
		t.Fatal(err)
	}
	if tools := tts.Tools(); len(tools) != 0 {
		t.Fatalf("disabled tts offers %v", tools)
	}
}
