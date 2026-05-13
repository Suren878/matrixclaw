package telegram

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterCommandsUsesSharedTopLevelMenu(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	worker.registerCommands(context.Background())

	if len(api.setCommandsRequests) != 1 {
		t.Fatalf("setCommandsRequests len = %d, want 1", len(api.setCommandsRequests))
	}
	var got []string
	var descriptions []string
	for _, command := range api.setCommandsRequests[0].Commands {
		got = append(got, command.Command)
		descriptions = append(descriptions, command.Description)
	}
	want := []string{"sessions", "context", "modules", "tasks", "server", "help"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("registered commands = %#v, want %#v", got, want)
	}
	wantDescriptions := []string{"Sessions", "Context", "Modules", "Tasks", "Server", "Help"}
	if strings.Join(descriptions, ",") != strings.Join(wantDescriptions, ",") {
		t.Fatalf("registered descriptions = %#v, want %#v", descriptions, wantDescriptions)
	}
}
