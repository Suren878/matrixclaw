package telegram

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func TestPickerKeyboardAddsBackFooter(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerMCP, "External MCP").
		Back("/modules").
		Row("enabled", "External MCP", "Enabled", "/modules mcp enabled").
		Build()
	markup := pickerKeyboardForTest(picker)
	if markup == nil || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("pickerKeyboard() = %#v, want rows", markup)
	}
	lastRow := markup.InlineKeyboard[len(markup.InlineKeyboard)-1]
	if len(lastRow) != 1 {
		t.Fatalf("last row = %#v, want one Back button", lastRow)
	}
	button := lastRow[0]
	if button.Text != "‹ Back" {
		t.Fatalf("Back button text = %q", button.Text)
	}
	_, command, ok := parsePickerCallbackData(button.CallbackData)
	if !ok {
		t.Fatalf("Back callback did not parse: %q", button.CallbackData)
	}
	if command != "/modules" {
		t.Fatalf("Back command = %q, want /modules", command)
	}
}

func TestPickerKeyboardAddsCloseFooterForRootPicker(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerModules, "Modules").
		Row("mcp", "External MCP", "", "/modules mcp").
		Build()
	markup := pickerKeyboardForTest(picker)
	if markup == nil || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("pickerKeyboard() = %#v, want rows", markup)
	}
	lastRow := markup.InlineKeyboard[len(markup.InlineKeyboard)-1]
	if len(lastRow) != 1 {
		t.Fatalf("last row = %#v, want one Close button", lastRow)
	}
	button := lastRow[0]
	if button.Text != "‹ Close" {
		t.Fatalf("Close button text = %q", button.Text)
	}
	kind, command, ok := parsePickerCallbackData(button.CallbackData)
	if !ok {
		t.Fatalf("Back callback did not parse: %q", button.CallbackData)
	}
	if kind != callbackKindDismiss || command != "" {
		t.Fatalf("Back callback = (%q, %q), want dismiss", kind, command)
	}
}

func TestFooterButtonUsesEmptyCommandForDismiss(t *testing.T) {
	button := footerButton(controlplane.ResultViewFooter{
		Label:   "Close",
		Command: "",
		Kind:    controlplane.FooterClose,
	})

	if button.Text != "‹ Close" {
		t.Fatalf("Close button text = %q", button.Text)
	}
	kind, command, ok := parsePickerCallbackData(button.CallbackData)
	if !ok {
		t.Fatalf("Close callback did not parse: %q", button.CallbackData)
	}
	if kind != callbackKindDismiss || command != "" {
		t.Fatalf("Close callback = (%q, %q), want dismiss with empty command", kind, command)
	}
}

func TestPickerKeyboardAddsCloseFooterForCommandMenu(t *testing.T) {
	picker := *controlplane.CommandMenuPicker(controlplane.MenuState{})
	markup := pickerKeyboardForTest(picker)
	if markup == nil || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("pickerKeyboard() = %#v, want rows", markup)
	}
	lastRow := markup.InlineKeyboard[len(markup.InlineKeyboard)-1]
	if len(lastRow) != 1 {
		t.Fatalf("last row = %#v, want one Close button", lastRow)
	}
	button := lastRow[0]
	if button.Text != "‹ Close" {
		t.Fatalf("Close button text = %q", button.Text)
	}
	kind, command, ok := parsePickerCallbackData(button.CallbackData)
	if !ok {
		t.Fatalf("Back callback did not parse: %q", button.CallbackData)
	}
	if kind != callbackKindDismiss || command != "" {
		t.Fatalf("Back callback = (%q, %q), want dismiss", kind, command)
	}
}

func TestPickerKeyboardTreatsCloseFooterAsClose(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerBrowser, "Browser Provider").
		Close("/modules browser").
		Row("disabled", "Disabled", "", "/modules browser set-provider disabled").
		Build()
	markup := pickerKeyboardForTest(picker)
	if markup == nil || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("pickerKeyboard() = %#v, want rows", markup)
	}
	lastRow := markup.InlineKeyboard[len(markup.InlineKeyboard)-1]
	if len(lastRow) != 1 {
		t.Fatalf("last row = %#v, want one Close button", lastRow)
	}
	button := lastRow[0]
	if button.Text != "‹ Close" {
		t.Fatalf("Close button text = %q", button.Text)
	}
	_, command, ok := parsePickerCallbackData(button.CallbackData)
	if !ok {
		t.Fatalf("Close callback did not parse: %q", button.CallbackData)
	}
	if command != "/modules browser" {
		t.Fatalf("Close command = %q, want /modules browser", command)
	}
}

func TestPickerKeyboardAddsSelectCloseFooter(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerMCPServer, "Enabled").
		Context("docs").
		Select("/modules mcp docs").
		Row("on", "On", "", "/modules mcp docs set-enabled on").
		Build()
	markup := pickerKeyboardForTest(picker)
	if markup == nil || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("pickerKeyboard() = %#v, want rows", markup)
	}
	lastRow := markup.InlineKeyboard[len(markup.InlineKeyboard)-1]
	if len(lastRow) != 1 {
		t.Fatalf("last row = %#v, want one Close button", lastRow)
	}
	button := lastRow[0]
	if button.Text != "‹ Close" {
		t.Fatalf("Close button text = %q", button.Text)
	}
	_, command, ok := parsePickerCallbackData(button.CallbackData)
	if !ok {
		t.Fatalf("Close callback did not parse: %q", button.CallbackData)
	}
	if command != "/modules mcp docs" {
		t.Fatalf("Close command = %q, want /modules mcp docs", command)
	}
}

func TestPickerKeyboardMarksSelectedItems(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerProvider, "Provider").
		Item(controlplane.PickerItem{ID: "openai", Title: "OpenAI", Command: "/provider use openai", Selected: true}).
		Item(controlplane.PickerItem{ID: "anthropic", Title: "Anthropic", Command: "/provider use anthropic"}).
		Build()

	markup := pickerKeyboardForTest(picker)
	if markup == nil || len(markup.InlineKeyboard) == 0 || len(markup.InlineKeyboard[0]) != 1 {
		t.Fatalf("pickerKeyboard() = %#v, want first item row", markup)
	}
	button := markup.InlineKeyboard[0][0]
	want := controlplane.SelectedMarker + " 🔌 OpenAI"
	if button.Text != want {
		t.Fatalf("selected button text = %q, want %q", button.Text, want)
	}
}

func pickerKeyboardForTest(picker controlplane.PickerData) *InlineKeyboardMarkup {
	view := controlplane.PresentResult(controlplane.Result{Handled: true, Picker: &picker}, controlplane.ResultViewOptions{
		Surface:  controlplane.SurfaceTelegram,
		Page:     -1,
		PageSize: modelPickerPageSize,
	})
	return pickerKeyboardView(picker, view)
}
