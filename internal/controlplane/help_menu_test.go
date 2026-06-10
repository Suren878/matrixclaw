package controlplane

import (
	"context"
	"testing"
)

func TestHelpCommandReturnsCommandMenuPicker(t *testing.T) {
	result, err := New(struct{}{}, "").Handle(context.Background(), "", "/help")
	if err != nil {
		t.Fatalf("Handle(/help) error = %v", err)
	}
	if result.Picker == nil {
		t.Fatalf("Handle(/help) picker = nil, want command menu")
	}
	if result.Picker.Kind != PickerCommandMenu {
		t.Fatalf("Handle(/help) picker kind = %q, want %q", result.Picker.Kind, PickerCommandMenu)
	}
	if len(result.Picker.Items) == 0 {
		t.Fatal("Handle(/help) picker has no command items")
	}
	if footer, ok := PickerFooter(*result.Picker); ok {
		t.Fatalf("command menu footer = %#v, want none", footer)
	}
}
