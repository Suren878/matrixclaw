package dialog

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

func TestPickerNavigationSkipsHeadersAndDividers(t *testing.T) {
	dialog := NewPicker(surfacecommon.DefaultCommon(), PickerData{
		Title: "Sessions",
		Entries: []PickerEntry{
			{Kind: ListEntryHeader, Title: "Session"},
			{ID: "new", Title: "Create New", Action: ActionRunControlplaneCommand{Command: "/new"}},
			{ID: "session-1", Title: "Docs", Action: ActionRunControlplaneCommand{Command: "/session menu session-1"}},
			{Kind: ListEntryDivider, ID: "divider"},
			{ID: "cancel", Title: "Cancel", Action: ActionClose{}},
		},
	})

	selected := dialog.selectedOption()
	if selected == nil {
		t.Fatal("selected item is nil")
	}
	if selected.item.ID != "new" {
		t.Fatalf("selected item id = %q, want new", selected.item.ID)
	}

	_ = dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	selected = dialog.selectedOption()
	if selected == nil {
		t.Fatal("selected item is nil")
	}
	if selected.item.ID != "session-1" {
		t.Fatalf("selected item id = %q, want session-1", selected.item.ID)
	}
}

func TestPickerCancelReturnsCloseAction(t *testing.T) {
	dialog := NewPicker(surfacecommon.DefaultCommon(), PickerData{
		Title: "Provider",
		Entries: []PickerEntry{
			{ID: "openai", Title: "OpenAI", Action: ActionRunControlplaneCommand{Command: "/provider openai"}},
			{Kind: ListEntryDivider, ID: "divider"},
			{ID: "cancel", Title: "Cancel", Action: ActionClose{}},
		},
	})

	_ = dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := action.(ActionClose); !ok {
		t.Fatalf("action = %T, want ActionClose", action)
	}
}

func TestPickerEscapeUsesConfiguredCloseAction(t *testing.T) {
	dialog := NewPicker(surfacecommon.DefaultCommon(), PickerData{
		Title:       "Provider Actions",
		CloseAction: ActionRunControlplaneCommand{Command: "/provider"},
		Entries: []PickerEntry{
			{ID: "use", Title: "Use", Action: ActionRunControlplaneCommand{Command: "/provider use local-ai"}},
		},
	})

	action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEsc})
	run, ok := action.(ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("action = %T, want ActionRunControlplaneCommand", action)
	}
	if run.Command != "/provider" {
		t.Fatalf("command = %q, want /provider", run.Command)
	}
}

func TestPickerVisibleWindowCentersSelection(t *testing.T) {
	entries := make([]PickerEntry, 0, 30)
	for i := 0; i < 30; i++ {
		entries = append(entries, PickerEntry{
			ID:     strconv.Itoa(i),
			Title:  "Model",
			Action: ActionClose{},
		})
	}
	dialog := NewPicker(surfacecommon.DefaultCommon(), PickerData{
		Title:   "Models",
		Entries: entries,
	})
	dialog.cursor = 15

	items, selected, footer, footerSelected := dialog.visibleWindow(7)
	if len(items) != 7 {
		t.Fatalf("len(items) = %d, want 7", len(items))
	}
	if selected != 3 {
		t.Fatalf("selected = %d, want centered index 3", selected)
	}
	if items[selected].ID != entries[15].ID {
		t.Fatalf("selected item = %q, want %q", items[selected].ID, entries[15].ID)
	}
	if len(footer) != 0 || footerSelected != -1 {
		t.Fatalf("footer = %#v selected=%d, want empty/-1", footer, footerSelected)
	}
}

func TestPickerInitialSelectionUsesSelectedEntry(t *testing.T) {
	dialog := NewPicker(surfacecommon.DefaultCommon(), PickerData{
		Title: "Models",
		Entries: []PickerEntry{
			{ID: "m1", Title: "m1", Action: ActionClose{}},
			{ID: "m2", Title: "m2", Selected: true, Action: ActionClose{}},
			{ID: "m3", Title: "m3", Action: ActionClose{}},
		},
	})

	selected := dialog.selectedOption()
	if selected == nil {
		t.Fatal("selected item is nil")
	}
	if selected.item.ID != "m2" {
		t.Fatalf("selected item id = %q, want m2", selected.item.ID)
	}
}
