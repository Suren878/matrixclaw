package controlplane

import "testing"

func TestPickerFooterUsesBackAndCloseState(t *testing.T) {
	backPicker := NewPickerData(PickerMCP, "External MCP").Back(modulesCommand()).Build()
	footer, ok := PickerFooter(backPicker)
	if !ok {
		t.Fatal("PickerFooter() ok = false, want true")
	}
	if footer.Label != "Back" || footer.Command != modulesCommand() {
		t.Fatalf("back footer = %#v", footer)
	}

	closePicker := NewPickerData(PickerServer, "Server").Close(serverCommand()).Build()
	footer, ok = PickerFooter(closePicker)
	if !ok {
		t.Fatal("PickerFooter() close ok = false, want true")
	}
	if footer.Label != "Close" || footer.Command != serverCommand() {
		t.Fatalf("close footer = %#v", footer)
	}

	plainPicker := NewPickerData(PickerMCP, "External MCP").Build()
	if footer, ok := PickerFooter(plainPicker); ok {
		t.Fatalf("PickerFooter() = %#v, true; want false", footer)
	}

	popupPicker := PickerData{Kind: PickerMCPServer, ContextID: "docs", Popup: true}
	footer, ok = PickerFooter(popupPicker)
	if ok {
		t.Fatalf("PickerFooter() popup = %#v, true; want false", footer)
	}

	selectPicker := NewPickerData(PickerMCPServer, "Enabled").
		Context("docs").
		Select(mcpServerCommand("docs")).
		Build()
	footer, ok = PickerFooter(selectPicker)
	if !ok {
		t.Fatal("PickerFooter() select ok = false, want true")
	}
	if footer.Label != "Close" || footer.Command != mcpServerCommand("docs") {
		t.Fatalf("PickerFooter() select = %#v, want Close to %q", footer, mcpServerCommand("docs"))
	}
}
