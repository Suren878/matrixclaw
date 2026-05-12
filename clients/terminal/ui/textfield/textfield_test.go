package textfield

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTextFieldAcceptsPlaceholderAndPaste(t *testing.T) {
	field := New("API key", "", WithCharLimit(64))

	if field.Placeholder() != "API key" {
		t.Fatalf("Placeholder() = %q, want API key", field.Placeholder())
	}
	field.Update(tea.PasteMsg{Content: "sk-test"})
	if field.Value() != "sk-test" {
		t.Fatalf("Value() = %q, want pasted text", field.Value())
	}
}

func TestTextFieldSanitizesSingleLinePaste(t *testing.T) {
	field := New("Model", "")

	field.Update(tea.PasteMsg{Content: "line one\nline two\tline three"})
	if field.Value() != "line one line two line three" {
		t.Fatalf("Value() = %q, want sanitized single line", field.Value())
	}
}
