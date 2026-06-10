package telegram

import (
	"context"
	"testing"
)

func TestRegisterCommandsDoesNotPublishTTS(t *testing.T) {
	api := &deliveryFakeBotAPI{}
	worker := &Worker{api: api}

	worker.registerCommands(context.Background())

	if len(api.commands) != 1 {
		t.Fatalf("SetMyCommands calls = %d, want 1", len(api.commands))
	}
	var hasModules bool
	for _, command := range api.commands[0].Commands {
		if command.Command == "tts" {
			t.Fatalf("registered hidden tts command: %#v", api.commands[0].Commands)
		}
		if command.Command == "modules" {
			hasModules = true
		}
	}
	if !hasModules {
		t.Fatalf("registered commands missing modules: %#v", api.commands[0].Commands)
	}
}
