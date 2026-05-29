package controlplane

import (
	"strconv"
	"strings"
	"testing"
)

func TestPickerItemCommandDoesNotSynthesizeFallback(t *testing.T) {
	picker := PickerData{Kind: PickerSessions, ContextID: "s1"}
	item := PickerItem{ID: "s2", Title: "Session"}

	if got := PickerItemCommand(picker, item); got != "" {
		t.Fatalf("PickerItemCommand = %q, want empty command", got)
	}
}

func TestAssertPickerCommandsAllowsDisabledInformationalItems(t *testing.T) {
	picker := &PickerData{Kind: PickerSkills, Items: []PickerItem{
		{ID: "empty", Title: "No skills", Disabled: true},
		{ID: "library", Title: "Skill Library", Command: skillsCommand("library")},
	}}

	assertPickerCommands(t, picker)
}

func TestAssertPickerCommandsRejectsEnabledInteractiveItemsWithoutCommand(t *testing.T) {
	picker := &PickerData{Kind: PickerSessions, Items: []PickerItem{{ID: "s1", Title: "Session"}}}

	err := pickerCommandValidationError(picker)
	if err == "" {
		t.Fatal("pickerCommandValidationError = empty, want missing command error")
	}
	if !strings.Contains(err, `item "s1"`) {
		t.Fatalf("pickerCommandValidationError = %q, want item id", err)
	}
}

func assertPickerCommands(t *testing.T, picker *PickerData) {
	t.Helper()
	if err := pickerCommandValidationError(picker); err != "" {
		t.Fatal(err)
	}
}

func pickerCommandValidationError(picker *PickerData) string {
	if picker == nil {
		return ""
	}
	for _, item := range picker.Items {
		if item.Disabled {
			continue
		}
		if strings.TrimSpace(item.Title) == "" && strings.TrimSpace(item.ID) == "" {
			continue
		}
		if strings.TrimSpace(item.Command) == "" {
			return "picker " + string(picker.Kind) + " item " + strconv.Quote(item.ID) + " has empty command"
		}
	}
	return ""
}
