package controlplane

import (
	"reflect"
	"strings"
	"testing"
)

func TestBotCommandSurfaceHidesTTSAndShowsModules(t *testing.T) {
	menu := CommandMenuView(SurfaceTelegramBotCommands, MenuState{})

	if commandViewContainsCommand(menu.Items, "/tts") {
		t.Fatalf("telegram bot command surface includes /tts: %#v", menu.Items)
	}
	if !commandViewContainsCommand(menu.Items, modulesCommand()) {
		t.Fatalf("telegram bot command surface missing %q: %#v", modulesCommand(), menu.Items)
	}
}

func TestTelegramAndTerminalCommandMenusShareControlplaneItems(t *testing.T) {
	state := MenuState{SessionTitle: "Main", ProviderID: "openai", ModelID: "gpt-5"}

	telegram := CommandMenuView(SurfaceTelegram, state)
	terminal := CommandMenuView(SurfaceTerminal, state)

	if !reflect.DeepEqual(commandViewIDs(telegram.Items), commandViewIDs(terminal.Items)) {
		t.Fatalf("telegram menu ids = %#v, terminal menu ids = %#v", commandViewIDs(telegram.Items), commandViewIDs(terminal.Items))
	}
	if commandViewContainsCommand(terminal.Items, "/tts") {
		t.Fatalf("terminal command menu includes /tts: %#v", terminal.Items)
	}
	if !commandViewContainsCommand(terminal.Items, modulesCommand()) {
		t.Fatalf("terminal command menu missing %q: %#v", modulesCommand(), terminal.Items)
	}
}

func TestPickerViewMarksSelectedItemsForTelegram(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind PickerKind
	}{
		{name: "provider", kind: PickerProvider},
		{name: "model", kind: PickerSessionModels},
		{name: "runtime mode", kind: PickerBrowser},
	} {
		t.Run(tt.name, func(t *testing.T) {
			picker := PickerData{
				Kind: tt.kind,
				Items: []PickerItem{
					{ID: "inactive", Title: "Inactive", Command: "/noop inactive"},
					{ID: "active", Title: "Active", Command: "/noop active", Selected: true},
				},
			}

			view := PickerView(picker, PickerViewOptions{Surface: SurfaceTelegram})
			active := requireResultViewItem(t, view.Items, "active")
			if !active.Selected {
				t.Fatalf("selected item Selected = false: %#v", active)
			}
			if !strings.HasPrefix(active.Label, SelectedMarker+" ") {
				t.Fatalf("selected item label = %q, want %q prefix", active.Label, SelectedMarker+" ")
			}

			inactive := requireResultViewItem(t, view.Items, "inactive")
			if strings.HasPrefix(inactive.Label, SelectedMarker+" ") {
				t.Fatalf("inactive item label = %q, want no selected marker", inactive.Label)
			}
		})
	}
}

func TestPresentResultReturnsPickerScreenAndFooterSemantics(t *testing.T) {
	root := NewPickerData(PickerProvider, "Provider").
		Row("openai", "OpenAI", "", "/provider use openai").
		Build()

	rootView := PresentResult(Result{Handled: true, Picker: &root}, ResultViewOptions{Surface: SurfaceTelegram})
	if rootView.Screen != ScreenPicker {
		t.Fatalf("root screen = %q, want %q", rootView.Screen, ScreenPicker)
	}
	if rootView.Footer == nil || rootView.Footer.Kind != FooterDismiss {
		t.Fatalf("root footer = %#v, want dismiss footer", rootView.Footer)
	}

	nested := NewPickerData(PickerMCP, "External MCP").
		Back(modulesCommand()).
		Row("server", "Server", "", "/modules mcp server").
		Build()

	nestedView := PresentResult(Result{Handled: true, Picker: &nested}, ResultViewOptions{Surface: SurfaceTelegram})
	if nestedView.Footer == nil {
		t.Fatal("nested footer = nil, want back footer")
	}
	if nestedView.Footer.Kind != FooterBack || nestedView.Footer.Command != modulesCommand() {
		t.Fatalf("nested footer = %#v, want back to %q", nestedView.Footer, modulesCommand())
	}
}

func TestPickerViewCarriesActionAndDangerRolesWithSeparators(t *testing.T) {
	picker := NewPickerData(PickerProviderActions, "Provider Actions").
		Row("use", "Use Provider", "", "/provider use openai").
		Action("edit", "Edit Provider", "", "/provider edit openai").
		Danger("delete", "Delete Provider", "", "/provider delete openai").
		Build()

	view := PickerView(picker, PickerViewOptions{Surface: SurfaceTerminal})

	edit := requireResultViewItem(t, view.Items, "edit")
	if edit.Role != PickerItemRoleAction {
		t.Fatalf("edit role = %q, want %q", edit.Role, PickerItemRoleAction)
	}
	if !edit.SeparatorBefore {
		t.Fatalf("edit SeparatorBefore = false, want true")
	}

	deleteItem := requireResultViewItem(t, view.Items, "delete")
	if deleteItem.Role != PickerItemRoleDanger {
		t.Fatalf("delete role = %q, want %q", deleteItem.Role, PickerItemRoleDanger)
	}
	if !deleteItem.SeparatorBefore {
		t.Fatalf("delete SeparatorBefore = false, want true")
	}
}

func TestPickerViewCarriesSearchText(t *testing.T) {
	picker := NewPickerData(PickerSkills, "Skills").
		Item(PickerItem{
			ID:      "deploy",
			Title:   "Deploy",
			Info:    "Installed",
			Search:  "deploy ci release linux",
			Command: "/modules skills deploy",
		}).
		Build()

	view := PickerView(picker, PickerViewOptions{Surface: SurfaceTerminal})

	item := requireResultViewItem(t, view.Items, "deploy")
	if item.Search != "deploy ci release linux" {
		t.Fatalf("search = %q, want custom search text", item.Search)
	}
}

func TestPickerViewTreatsCommandFooterAsBack(t *testing.T) {
	picker := NewPickerData(PickerServer, "Server").
		Close(serverCommand()).
		Row("status", "Status", "", "/server status").
		Build()

	view := PickerView(picker, PickerViewOptions{Surface: SurfaceTelegram})

	if view.Footer == nil {
		t.Fatal("footer = nil, want back footer")
	}
	if view.Footer.Kind != FooterBack || view.Footer.Label != "Back" || view.Footer.Command != serverCommand() {
		t.Fatalf("footer = %#v, want Back to %q", view.Footer, serverCommand())
	}
}

func TestPresentResultReturnsTextEditScreen(t *testing.T) {
	result := Result{Handled: true, TextEdit: &TextEditData{
		Title:               "Skill Instructions",
		Placeholder:         "Write instructions",
		Value:               "body",
		SubmitCommandPrefix: "/modules skills save-body",
		CancelCommand:       "/modules skills",
	}}

	view := PresentResult(result, ResultViewOptions{Surface: SurfaceTerminal})

	if view.Screen != ScreenTextEdit {
		t.Fatalf("screen = %q, want %q", view.Screen, ScreenTextEdit)
	}
	if view.Title != "Skill Instructions" {
		t.Fatalf("title = %q, want Skill Instructions", view.Title)
	}
	if view.Text != "body" {
		t.Fatalf("text = %q, want body", view.Text)
	}
	if view.Footer == nil || view.Footer.Kind != FooterBack || view.Footer.Command != "/modules skills" {
		t.Fatalf("footer = %#v, want back to /modules skills", view.Footer)
	}
}

func commandViewContainsCommand(items []ResultViewItem, command string) bool {
	for _, item := range items {
		if item.Command == command {
			return true
		}
	}
	return false
}

func commandViewIDs(items []ResultViewItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func requireResultViewItem(t *testing.T, items []ResultViewItem, id string) ResultViewItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("item %q not found in %#v", id, items)
	return ResultViewItem{}
}
