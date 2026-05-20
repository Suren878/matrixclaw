package commandui

import "testing"

func TestListStateEmitsSemanticEvents(t *testing.T) {
	state := ListState{}
	items := []Item{
		{ID: "open", Title: "Open"},
		{ID: "exit", Title: "Exit", Role: RoleExit},
	}

	if event := state.Update("enter", items, RoleExit); event.Kind != EventSelect || event.ID != "open" {
		t.Fatalf("enter on first item = %+v, want select open", event)
	}
	state.Cursor = 1
	if event := state.Update("enter", items, RoleExit); event.Kind != EventExit {
		t.Fatalf("enter on exit = %+v, want exit", event)
	}
	if event := state.Update("esc", items, RoleBack); event.Kind != EventBack {
		t.Fatalf("esc = %+v, want back", event)
	}
}

func TestListStateEmitsShortcutEvents(t *testing.T) {
	state := ListState{}
	items := []Item{
		{ID: "providers", Title: "Providers", Shortcut: "ctrl+p"},
		{ID: "disabled", Title: "Disabled", Shortcut: "ctrl+d", Disabled: true},
	}

	if event := state.Update("ctrl+p", items, RoleExit); event.Kind != EventSelect || event.ID != "providers" {
		t.Fatalf("shortcut event = %+v, want select providers", event)
	}
	if event := state.Update("ctrl+d", items, RoleExit); !event.IsZero() {
		t.Fatalf("disabled shortcut event = %+v, want none", event)
	}
}

func TestListStateSkipsHeadersAndDividers(t *testing.T) {
	state := ListState{}
	items := []Item{
		Header("Group"),
		{ID: "first", Title: "First"},
		Divider("gap"),
		{ID: "second", Title: "Second"},
	}

	state.Update("down", items, RoleBack)
	if state.Cursor != 1 {
		t.Fatalf("cursor after down = %d, want first selectable item", state.Cursor)
	}
	state.Update("down", items, RoleBack)
	if state.Cursor != 3 {
		t.Fatalf("cursor after second down = %d, want second selectable item", state.Cursor)
	}
	if event := state.Update("enter", items, RoleBack); event.Kind != EventSelect || event.ID != "second" {
		t.Fatalf("enter on selected row = %+v, want select second", event)
	}
	state.Cursor = 2
	if event := state.Update("enter", items, RoleBack); !event.IsZero() {
		t.Fatalf("enter on divider = %+v, want none", event)
	}
}

func TestListStateCanDisableWrapping(t *testing.T) {
	state := ListState{NoWrap: true}
	items := []Item{{ID: "first", Title: "First"}, {ID: "second", Title: "Second"}}

	state.Update("up", items, RoleBack)
	if state.Cursor != 0 {
		t.Fatalf("cursor after no-wrap up = %d, want 0", state.Cursor)
	}
	state.Cursor = 1
	state.Update("down", items, RoleBack)
	if state.Cursor != 1 {
		t.Fatalf("cursor after no-wrap down = %d, want 1", state.Cursor)
	}
}

func TestFormStateUsesStructuredFocus(t *testing.T) {
	state := FormState{Focus: FormFocus{Kind: FormFocusField}}
	fields := []Item{{ID: "name", Title: "Name"}}
	buttons := []ButtonSpec{
		{Label: "Save", Role: RoleSubmit},
		{Label: "Back", Role: RoleBack},
	}

	if event := state.Update("enter", fields, buttons, RoleBack); event.Kind != EventEdit || event.ID != "name" {
		t.Fatalf("enter on field = %+v, want edit name", event)
	}
	state.Update("down", fields, buttons, RoleBack)
	if state.Focus.Kind != FormFocusButton {
		t.Fatalf("focus = %+v, want button focus", state.Focus)
	}
	if event := state.Update("enter", fields, buttons, RoleBack); event.Kind != EventSubmit {
		t.Fatalf("enter on save = %+v, want submit", event)
	}
	state.Update("right", fields, buttons, RoleBack)
	if event := state.Update("enter", fields, buttons, RoleBack); event.Kind != EventBack {
		t.Fatalf("enter on back = %+v, want back", event)
	}
}

func TestFormStateCanDisableWrapping(t *testing.T) {
	state := FormState{Focus: FormFocus{Kind: FormFocusField}, NoWrap: true}
	fields := []Item{{ID: "name", Title: "Name"}}
	buttons := []ButtonSpec{{Label: "Save", Role: RoleSubmit}}

	state.Update("up", fields, buttons, RoleBack)
	if state.Focus.Kind != FormFocusField || state.Focus.Index != 0 {
		t.Fatalf("focus after no-wrap up = %+v, want first field", state.Focus)
	}
	state.Update("down", fields, buttons, RoleBack)
	if state.Focus.Kind != FormFocusButton {
		t.Fatalf("focus after down = %+v, want button", state.Focus)
	}
	state.Update("down", fields, buttons, RoleBack)
	if state.Focus.Kind != FormFocusButton {
		t.Fatalf("focus after no-wrap down = %+v, want button", state.Focus)
	}
}

func TestConfirmStateUsesSharedEvents(t *testing.T) {
	state := ConfirmState{}
	if event := state.Update("enter"); event.Kind != EventSubmit {
		t.Fatalf("enter on confirm = %+v, want submit", event)
	}
	state.Update("right")
	if state.Selected != 1 {
		t.Fatalf("selected after right = %d, want cancel", state.Selected)
	}
	if event := state.Update("enter"); event.Kind != EventCancel {
		t.Fatalf("enter on cancel = %+v, want cancel", event)
	}
	if event := state.Update("y"); event.Kind != EventSubmit {
		t.Fatalf("y = %+v, want submit", event)
	}
	if event := state.Update("n"); event.Kind != EventCancel {
		t.Fatalf("n = %+v, want cancel", event)
	}
}

func TestTextEditStateUsesSharedButtonEvents(t *testing.T) {
	state := TextEditState{ButtonsFocused: true}
	buttons := []ButtonSpec{
		{Label: "Save", Role: RoleSubmit},
		{Label: "Cancel", Role: RoleCancel},
	}

	if event := state.Update("enter", buttons, RoleBack); event.Kind != EventSubmit {
		t.Fatalf("enter on save = %+v, want submit", event)
	}
	state.Update("right", buttons, RoleBack)
	if event := state.Update("enter", buttons, RoleBack); event.Kind != EventCancel {
		t.Fatalf("enter on cancel = %+v, want cancel", event)
	}
	if event := (&TextEditState{ButtonsFocused: true}).Update("enter", nil, RoleBack); !event.IsZero() {
		t.Fatalf("enter without buttons = %+v, want none", event)
	}
}
