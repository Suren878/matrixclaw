package commandmenu

import (
	"testing"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func TestEntriesHideMemoryCommand(t *testing.T) {
	for _, entry := range Entries(State{}) {
		if entry.ID == string(controlplane.CommandMemory) {
			t.Fatalf("Entries() included %q, want memory hidden from command menu", entry.ID)
		}
	}
}

func TestEntriesContextOpensContextPicker(t *testing.T) {
	entry := requireCommandEntry(t, Entries(State{}), string(controlplane.CommandContext))
	if entry.Title != "Context" {
		t.Fatalf("context title = %q, want Context", entry.Title)
	}
	action, ok := entry.Action.(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("context action = %T, want ActionRunControlplaneCommand", entry.Action)
	}
	if action.Command != "/context" {
		t.Fatalf("context command = %q, want /context", action.Command)
	}
}

func TestPickerEntriesMarkSelectedRowsAccent(t *testing.T) {
	picker := controlplane.PickerData{Items: []controlplane.PickerItem{{
		ID:       "active",
		Title:    "Active",
		Command:  "/session menu active",
		Selected: true,
	}}}

	entry := requirePickerEntry(t, PickerRows(picker), "active")
	if entry.Tone != components.RowToneAccent {
		t.Fatalf("selected picker tone = %v, want accent", entry.Tone)
	}
}

func requireCommandEntry(t *testing.T, entries []surfacedialog.CommandEntry, id string) surfacedialog.CommandEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("command entry %q not found in %#v", id, entries)
	return surfacedialog.CommandEntry{}
}

func requirePickerEntry(t *testing.T, entries []surfacedialog.PickerEntry, id string) surfacedialog.PickerEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("picker entry %q not found in %#v", id, entries)
	return surfacedialog.PickerEntry{}
}
