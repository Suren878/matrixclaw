package controlplane

import "testing"

func TestPickerViewItemsAddsCommandsAndSeparators(t *testing.T) {
	items := PickerViewItems(PickerData{
		Kind:        PickerProviderActions,
		ContextID:   "local-ai",
		BackCommand: "/provider",
		HasBack:     true,
		Items: []PickerItem{
			{ID: "use", Title: "Use", Command: "/provider use local-ai"},
			{ID: "delete", Title: "Delete", Role: PickerItemRoleDanger, Command: "/provider custom delete local-ai"},
			{ID: "back", Title: "Back", Role: PickerItemRoleBack},
		},
	})
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	if items[0].Command != "/provider use local-ai" {
		t.Fatalf("use command = %q", items[0].Command)
	}
	if !items[1].SeparatorBefore {
		t.Fatal("expected separator before destructive item")
	}
	if items[2].Command != "/provider" {
		t.Fatalf("back command = %q, want /provider", items[2].Command)
	}
}

func TestPickerItemCommandPrefersExplicitCommand(t *testing.T) {
	picker := PickerData{Kind: PickerProviderActions, ContextID: "local-ai", BackCommand: "/provider", HasBack: true}
	item := PickerItem{ID: "use", Command: "/custom command"}
	if got := PickerItemCommand(picker, item); got != "/custom command" {
		t.Fatalf("PickerItemCommand = %q, want explicit command", got)
	}
}

func TestPickerBuilderAutoCommandsUseFinalContext(t *testing.T) {
	picker := NewPickerData(PickerProviderActions, "Local AI").
		Row("use", "Use", "").
		Danger("delete", "Delete", "").
		Context("local-ai").
		Build()

	if got := picker.Items[0].Command; got != "/provider use local-ai" {
		t.Fatalf("row command = %q, want /provider use local-ai", got)
	}
	if got := picker.Items[1].Command; got != "/provider custom delete local-ai" {
		t.Fatalf("danger command = %q, want /provider custom delete local-ai", got)
	}
}

func TestPickerBuilderPreservesExplicitAndPlainItems(t *testing.T) {
	picker := NewPickerData(PickerProviderCustom, "Tool Use").
		Row("native", "Enabled", "", "/provider custom openai set-tools token native").
		Items(PickerItem{ID: "openai", Title: "OpenAI Compatible"}).
		Build()

	if got := picker.Items[0].Command; got != "/provider custom openai set-tools token native" {
		t.Fatalf("explicit row command = %q, want custom tool-mode command", got)
	}
	if got := picker.Items[1].Command; got != "" {
		t.Fatalf("plain item command = %q, want empty", got)
	}
}

func TestPickerCloseCommandUsesBackBeforeExplicitClose(t *testing.T) {
	if got := PickerCloseCommand(PickerData{CloseCommand: "/close", BackCommand: "/back", HasClose: true, HasBack: true}); got != "/back" {
		t.Fatalf("close command = %q, want /back", got)
	}
	if got := PickerCloseCommand(PickerData{BackCommand: "/back", HasBack: true}); got != "/back" {
		t.Fatalf("close command = %q, want /back", got)
	}
	if got := PickerCloseCommand(PickerData{Kind: PickerProviderCustom, CloseCommand: "", HasClose: true}); got != "" {
		t.Fatalf("explicit empty close command = %q, want empty", got)
	}
	if got := PickerCloseCommand(PickerData{Kind: PickerProviderCustom, BackCommand: "", HasBack: true}); got != "" {
		t.Fatalf("explicit empty back command = %q, want empty", got)
	}
}

func TestPickerItemCommandHonorsExplicitEmptyNavigation(t *testing.T) {
	picker := PickerData{
		Kind:         PickerProviderCustom,
		BackCommand:  "",
		CloseCommand: "",
		HasBack:      true,
		HasClose:     true,
	}
	if got := PickerItemCommand(picker, BackItem()); got != "" {
		t.Fatalf("empty back command = %q, want no fallback command", got)
	}
	if got := PickerItemCommand(picker, CloseItem()); got != "" {
		t.Fatalf("empty close command = %q, want no fallback command", got)
	}
}

func TestPickerBuilderPreservesExplicitEmptyNavigation(t *testing.T) {
	stackBack := NewPickerData(PickerProviderCustom, "Model").
		Back("").
		Row("gpt-next", "gpt-next", "", "/provider edit set model token gpt-next").
		Build()
	if !stackBack.HasBack || !stackBack.HasClose {
		t.Fatalf("stack back picker flags = back:%v close:%v, want explicit flags", stackBack.HasBack, stackBack.HasClose)
	}
	if got := PickerCloseCommand(stackBack); got != "" {
		t.Fatalf("stack back close command = %q, want empty", got)
	}

	stackClose := NewPickerData(PickerStorage, "Storage").
		Back("/modules").
		Close("").
		Row("files", "Files", "").
		Build()
	if !stackClose.HasBack || !stackClose.HasClose {
		t.Fatalf("stack close picker flags = back:%v close:%v, want explicit flags", stackClose.HasBack, stackClose.HasClose)
	}
	if got := PickerItemCommand(stackClose, BackItem()); got != "/modules" {
		t.Fatalf("stack close back item command = %q, want /modules", got)
	}
	if got := PickerCloseCommand(stackClose); got != "/modules" {
		t.Fatalf("stack close command = %q, want /modules", got)
	}
}

func TestPaginatePickerKeepsCancelTrailingAndStartsAtSelected(t *testing.T) {
	picker := PickerData{Kind: PickerProvider}
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5"} {
		picker.Items = append(picker.Items, PickerItem{ID: id, Title: id, Selected: id == "m4"})
	}
	picker.Items = append(picker.Items, PickerItem{ID: "cancel", Title: "Cancel", Role: PickerItemRoleCancel})

	page := PaginatePicker(picker, -1, 2)
	if page.Page != 1 || page.Pages != 3 {
		t.Fatalf("page = %d/%d, want 1/3", page.Page, page.Pages)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "m3" || page.Items[1].ID != "m4" {
		t.Fatalf("page items = %#v, want m3,m4", page.Items)
	}
	if len(page.Trailing) != 1 || page.Trailing[0].ID != "cancel" {
		t.Fatalf("trailing = %#v, want cancel", page.Trailing)
	}
}
