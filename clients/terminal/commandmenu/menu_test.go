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

func TestEntriesExposeSharedModulesCommandAndNoTTS(t *testing.T) {
	entries := Entries(State{})
	requireCommandEntry(t, entries, string(controlplane.CommandModules))
	for _, entry := range entries {
		action, ok := entry.Action.(surfacedialog.ActionRunControlplaneCommand)
		if ok && action.Command == "/tts" {
			t.Fatalf("Entries() included hidden /tts shortcut: %#v", entry)
		}
	}
}

func TestPickerEntriesMarkSelectedRowsAccent(t *testing.T) {
	picker := controlplane.PickerData{Items: []controlplane.PickerItem{{
		ID:       "active",
		Title:    "Active",
		Command:  "/session menu active",
		Selected: true,
	}}}

	entry := requirePickerEntry(t, PickerRows(pickerViewForTest(picker)), "active")
	if entry.Tone != components.RowToneAccent {
		t.Fatalf("selected picker tone = %v, want accent", entry.Tone)
	}
}

func TestPickerEntriesUseViewSearchText(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerSkills, "Skills").
		Item(controlplane.PickerItem{
			ID:      "deploy",
			Title:   "Deploy",
			Info:    "Installed",
			Search:  "deploy ci release linux",
			Command: "/modules skills deploy",
		}).
		Build()

	entry := requirePickerEntry(t, PickerRows(pickerViewForTest(picker)), "deploy")
	if entry.Search != "deploy ci release linux" {
		t.Fatalf("search = %q, want view search text", entry.Search)
	}
}

func TestPickerCloseActionUsesViewFooter(t *testing.T) {
	root := controlplane.NewPickerData(controlplane.PickerProvider, "Provider").
		Row("openai", "OpenAI", "", "/provider use openai").
		Build()
	if _, ok := PickerCloseAction(pickerViewForTest(root)).(surfacedialog.ActionClose); !ok {
		t.Fatalf("root close action = %T, want ActionClose", PickerCloseAction(pickerViewForTest(root)))
	}

	nested := controlplane.NewPickerData(controlplane.PickerMCP, "External MCP").
		Back("/modules").
		Row("enabled", "Enabled", "", "/modules mcp enabled").
		Build()
	action, ok := PickerCloseAction(pickerViewForTest(nested)).(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("nested close action = %T, want ActionRunControlplaneCommand", PickerCloseAction(pickerViewForTest(nested)))
	}
	if action.Command != "/modules" {
		t.Fatalf("nested close command = %q, want /modules", action.Command)
	}

	closeWithCommand := controlplane.NewPickerData(controlplane.PickerServer, "Server").
		Close("/server").
		Row("status", "Status", "", "/server status").
		Build()
	action, ok = PickerCloseAction(pickerViewForTest(closeWithCommand)).(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("close command action = %T, want ActionRunControlplaneCommand", PickerCloseAction(pickerViewForTest(closeWithCommand)))
	}
	if action.Command != "/server" {
		t.Fatalf("close command = %q, want /server", action.Command)
	}
}

func pickerViewForTest(picker controlplane.PickerData) controlplane.PickerViewData {
	return controlplane.PickerView(picker, controlplane.PickerViewOptions{Surface: controlplane.SurfaceTerminal})
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
