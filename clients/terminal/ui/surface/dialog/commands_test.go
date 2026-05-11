package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

func TestNewCommandsIncludesRuntimeSafeItems(t *testing.T) {
	dialog := NewCommands(surfacecommon.DefaultCommon(), CommandsData{Entries: testCommandEntries(false)})

	items := dialog.visible
	if len(items) < 5 {
		t.Fatalf("len(items) = %d, want at least 5", len(items))
	}

	first := items[0]
	if first.item.ID != "switch_session" {
		t.Fatalf("first item id = %q, want switch_session", first.item.ID)
	}

	selected := dialog.selectedOption()
	if selected == nil {
		t.Fatal("selected item is nil")
	}
	if selected.item.ID != "switch_session" {
		t.Fatalf("selected item id = %q, want switch_session", selected.item.ID)
	}
}

func TestNewCommandsUsesProvidedEntries(t *testing.T) {
	dialog := NewCommands(surfacecommon.DefaultCommon(), CommandsData{Entries: testCommandEntries(true)})

	found := false
	for _, item := range dialog.visible {
		if item.item.ID == "open_external_editor" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected external editor command")
	}
}

func TestCommandsSelectReturnsAction(t *testing.T) {
	dialog := NewCommands(surfacecommon.DefaultCommon(), CommandsData{Entries: testCommandEntries(false)})

	action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	run, ok := action.(ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("action = %T, want ActionRunControlplaneCommand", action)
	}
	if run.Command != "/sessions" {
		t.Fatalf("action.Command = %q, want /sessions", run.Command)
	}
}

func TestCommandsIgnoreSearchText(t *testing.T) {
	dialog := NewCommands(surfacecommon.DefaultCommon(), CommandsData{Entries: testCommandEntries(false)})

	_ = dialog.HandleMsg(tea.KeyPressMsg{Text: "e", Code: 'e'})
	_ = dialog.HandleMsg(tea.KeyPressMsg{Text: "x", Code: 'x'})
	_ = dialog.HandleMsg(tea.KeyPressMsg{Text: "i", Code: 'i'})
	_ = dialog.HandleMsg(tea.KeyPressMsg{Text: "t", Code: 't'})

	action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	run, ok := action.(ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("action = %T, want ActionRunControlplaneCommand", action)
	}
	if run.Command != "/sessions" {
		t.Fatalf("action.Command = %q, want /sessions", run.Command)
	}
}

func TestCommandsNavigationSkipsNonSelectableItems(t *testing.T) {
	dialog := NewCommands(surfacecommon.DefaultCommon(), CommandsData{Entries: testCommandEntries(false)})

	_ = dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	selected := dialog.selectedOption()
	if selected == nil {
		t.Fatal("selected item is nil")
	}
	if selected.item.ID != "switch_provider" {
		t.Fatalf("selected item id = %q, want switch_provider", selected.item.ID)
	}
}

func testCommandEntries(editorAvailable bool) []CommandEntry {
	entries := []CommandEntry{
		{ID: "switch_session", Title: "Sessions", Shortcut: "ctrl+s", Action: ActionRunControlplaneCommand{Command: "/sessions"}},
		{ID: "switch_provider", Title: "Provider", Action: ActionRunControlplaneCommand{Command: "/provider"}},
		{ID: "switch_model", Title: "Model", Action: ActionRunControlplaneCommand{Command: "/model"}},
		{Kind: ListEntryDivider, ID: "divider_interface"},
	}
	if editorAvailable {
		entries = append(entries, CommandEntry{ID: "open_external_editor", Title: "Open External Editor", Shortcut: "ctrl+o", Action: ActionExternalEditor{}})
	}
	entries = append(entries, CommandEntry{ID: "quit", Title: "Exit", Action: ActionQuit{}})
	return entries
}
