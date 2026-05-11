package controlplane

import (
	"strings"
	"testing"
)

func TestPickerPresentationPreservesSelectionStatusAndNavigation(t *testing.T) {
	picker := PickerData{
		Kind: PickerProvider,
		Items: []PickerItem{
			{ID: "openai", Title: "OpenAI"},
			{ID: "anthropic", Title: "Anthropic", Info: "Configured · claude-sonnet-4.5 · Active", Selected: true},
			CloseItem(),
		},
	}

	items := PresentPickerItems(picker)
	if len(items) != 3 {
		t.Fatalf("presentation items len = %d, want 3", len(items))
	}
	if got := PickerPresentationTitle(PickerData{Kind: PickerProvider}); got != "/provider" {
		t.Fatalf("PickerPresentationTitle fallback = %q, want /provider", got)
	}
	if got := PickerLegend(picker); got != "enter select · esc back" {
		t.Fatalf("PickerLegend = %q, want enter select legend", got)
	}
	if items[0].Command != "/provider openai" {
		t.Fatalf("provider command = %q, want /provider openai", items[0].Command)
	}
	if !items[1].Selected || !strings.Contains(items[1].Status, "Active") || !strings.Contains(items[1].Status, "claude-sonnet-4.5") {
		t.Fatalf("selected item = %#v, want active status with provider info", items[1])
	}
	if !strings.Contains(items[1].CompactLabel, "Anthropic") || !strings.Contains(items[1].CompactLabel, "claude-sonnet-4.5") {
		t.Fatalf("selected compact label = %q, want title and info", items[1].CompactLabel)
	}
	if !items[2].Item.IsCancel() || items[2].Command != "" || !items[2].SeparatorBefore {
		t.Fatalf("close item = %#v, want separated cancel item with no command", items[2])
	}
}

func TestPickerPresentationKeepsServerItemsRunnable(t *testing.T) {
	result := (&Dispatcher{}).handleServer()
	if result.Picker == nil {
		t.Fatal("expected server picker")
	}
	picker := *result.Picker

	items := PresentPickerItems(picker)
	if got := items[0].Command; got != "/status" {
		t.Fatalf("status command = %q, want /status", got)
	}
	if got := items[1].Command; got != "/restart" {
		t.Fatalf("restart command = %q, want /restart", got)
	}
	if got := PickerCloseCommand(picker); got != "/server" {
		t.Fatalf("server close command = %q, want /server", got)
	}
	if got := PickerLegend(picker); got != "enter run · esc back" {
		t.Fatalf("server legend = %q", got)
	}
}
