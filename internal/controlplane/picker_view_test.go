package controlplane

import "testing"

func TestPickerViewItemsAddsCommandsAndSeparators(t *testing.T) {
	items := PickerViewItems(PickerData{
		Kind:        PickerProviderActions,
		ContextID:   "local-ai",
		BackCommand: "/provider",
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
	picker := PickerData{Kind: PickerProviderActions, ContextID: "local-ai", BackCommand: "/provider"}
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
	picker := NewPickerData(PickerProviderCustom, "Tool Mode").
		Row("native", "Native", "", "/provider custom openai set-tools token native").
		Items(PickerItem{ID: "openai", Title: "OpenAI Compatible"}).
		Build()

	if got := picker.Items[0].Command; got != "/provider custom openai set-tools token native" {
		t.Fatalf("explicit row command = %q, want custom tool-mode command", got)
	}
	if got := picker.Items[1].Command; got != "" {
		t.Fatalf("plain item command = %q, want empty", got)
	}
}

func TestPickerCloseCommandUsesExplicitCloseThenBack(t *testing.T) {
	if got := PickerCloseCommand(PickerData{CloseCommand: "/close", BackCommand: "/back"}); got != "/close" {
		t.Fatalf("close command = %q, want /close", got)
	}
	if got := PickerCloseCommand(PickerData{BackCommand: "/back"}); got != "/back" {
		t.Fatalf("close command = %q, want /back", got)
	}
}

func TestPaginatePickerKeepsCancelTrailingAndStartsAtSelected(t *testing.T) {
	picker := PickerData{Kind: PickerModel}
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
