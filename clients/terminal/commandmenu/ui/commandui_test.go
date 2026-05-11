package commandui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderBasicSurfacesExposeCoreContent(t *testing.T) {
	frame := NewFrame(80, 16)

	tests := map[string]struct {
		view string
		want []string
	}{
		"form": {
			view: RenderFormCard(frame, FormData{
				Title:   "Telegram",
				Fields:  []Item{{Title: "Enabled", Status: "yes"}},
				Focus:   FormFocus{Kind: FormFocusButton},
				Buttons: []ButtonSpec{{Label: "Save", Role: RoleSubmit}, {Label: "Back", Role: RoleBack}},
				Button:  1,
				Error:   "token is required",
			}),
			want: []string{"Telegram", "Enabled", "Save", "Back", "token is required"},
		},
		"confirm": {
			view: RenderConfirmCard(frame, ConfirmData{
				Message:      "Delete this item?",
				ConfirmLabel: "Delete",
				CancelLabel:  "Back",
				Selected:     1,
			}),
			want: []string{"Delete", "Delete this item?", "Back", "enter confirm"},
		},
		"prompt": {
			view: RenderPromptCard(frame, PromptData{
				Title: "Name",
				Value: "Local AI",
			}),
			want: []string{"Name", "Local AI", "enter save"},
		},
		"picker": {
			view: RenderPickerCard(frame, PickerData{
				Title: "Enabled",
				Items: []Item{{Title: "Yes"}, {Title: "No"}},
			}),
			want: []string{"Enabled", "Yes", "No", "esc cancel"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assertContainsAll(t, tt.view, tt.want...)
		})
	}
}

func TestRenderCardUsesMatrixclawDefaultTitle(t *testing.T) {
	frame := NewFrame(80, 16)
	view := ansi.Strip(frame.RenderCard(FrameData{
		Body: []string{"Ready"},
	}))

	if !strings.Contains(view, "matrixclaw") {
		t.Fatalf("default frame title missing matrixclaw:\n%s", view)
	}
	oldBrand := "Matrix" + "Claw"
	if strings.Contains(view, oldBrand) {
		t.Fatalf("default frame title uses old casing:\n%s", view)
	}
}

func TestRenderSearchListOrdersTopSearchAndResults(t *testing.T) {
	frame := NewFrame(90, 20)
	view := ansi.Strip(RenderSearchListCard(frame, SearchListData{
		Title:       "Providers",
		SearchValue: "gem",
		TopItems:    []Item{{Title: "Continue", Role: RoleSubmit}},
		TopSelected: 0,
		Items:       []Item{{Title: "Google Gemini"}},
	}))
	lines := strings.Split(view, "\n")

	assertLineOrder(t, lines, "Continue", "gem")
	assertLineOrder(t, lines, "gem", "Google Gemini")
}

func TestRenderShortcutHelp(t *testing.T) {
	frame := NewFrame(90, 20)

	t.Run("search list hides disabled shortcuts", func(t *testing.T) {
		view := ansi.Strip(RenderSearchListCard(frame, SearchListData{
			Title: "Menu",
			Items: []Item{
				{Title: "Providers", Shortcut: "ctrl+p"},
				{Title: "Disabled", Shortcut: "ctrl+d", Disabled: true},
			},
		}))

		if !strings.Contains(view, "ctrl+p providers") {
			t.Fatalf("rendered search list should show active shortcut in help:\n%s", view)
		}
		if strings.Contains(view, "ctrl+d") {
			t.Fatalf("rendered search list should hide disabled shortcut:\n%s", view)
		}
	})
}

func TestRenderListSupportsHeadersAndDividers(t *testing.T) {
	frame := NewFrame(90, 20)
	view := ansi.Strip(RenderPickerCard(frame, PickerData{
		Title: "Menu",
		Items: []Item{
			Header("Configured"),
			{Title: "OpenAI", Status: "Active"},
			Divider("available"),
			{Title: "Anthropic"},
		},
		Selected: 1,
	}))

	assertContainsAll(t, view, "Configured", "OpenAI", "Anthropic")
	if strings.Contains(view, "available") {
		t.Fatalf("divider id should not render as a selectable label:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	assertLineOrder(t, lines, "Configured", "OpenAI")
	assertLineOrder(t, lines, "OpenAI", "Anthropic")
}

func TestRenderInfoDoesNotSelectBodyRowsWhenFooterIsActive(t *testing.T) {
	frame := NewFrame(0, 0).WithInnerWidth(0)
	inactiveInfo := RenderInfoCard(frame, InfoData{
		Title:          "Context Usage",
		Items:          []Item{{Title: "Total", Status: "~12k tokens"}},
		Selected:       -1,
		Footer:         []Item{{Title: "Back", Role: RoleBack}},
		FooterSelected: 0,
		Help:           "esc/enter back",
	})
	bodySelectedInfo := RenderInfoCard(frame, InfoData{
		Title:    "Context Usage",
		Items:    []Item{{Title: "Total", Status: "~12k tokens"}},
		Selected: 0,
		Footer:   []Item{{Title: "Back", Role: RoleBack}},
		Help:     "esc/enter back",
	})

	inactiveTotal := findLine(strings.Split(inactiveInfo, "\n"), "Total")
	selectedTotal := findLine(strings.Split(bodySelectedInfo, "\n"), "Total")
	if inactiveTotal == "" || selectedTotal == "" {
		t.Fatalf("rendered info missing Total row:\n%s", inactiveInfo)
	}
	if inactiveTotal == selectedTotal {
		t.Fatalf("Total row should not use selected styling when selected is -1:\n%s", inactiveInfo)
	}
	if findLine(strings.Split(inactiveInfo, "\n"), "Back") == "" {
		t.Fatalf("rendered info missing Back footer:\n%s", inactiveInfo)
	}
}

func TestRenderInfoCardStaysCompactInsideLargeViewport(t *testing.T) {
	frame := NewFrame(120, 40)
	view := ansi.Strip(RenderInfoCard(frame, InfoData{
		Title:          "Context Usage",
		Items:          []Item{{Title: "Total", Status: "~12k tokens"}},
		Selected:       -1,
		Footer:         []Item{{Title: "Back", Role: RoleBack}},
		FooterSelected: 0,
		Help:           "esc/enter back",
	}))
	lines := strings.Split(view, "\n")

	if len(lines) >= 40 {
		t.Fatalf("info card height = %d, want compact card below viewport height:\n%s", len(lines), view)
	}
	topBorder := findLine(lines, "╔")
	if got := ansi.StringWidth(topBorder); got >= 120 {
		t.Fatalf("info card width = %d, want compact card below viewport width:\n%s", got, view)
	}
}

func TestRenderTextFieldTruncatesWithoutEllipsisAndRespectsInset(t *testing.T) {
	frame := NewFrame(90, 14).WithInnerWidth(0)
	line := ansi.Strip(RenderTextField(frame, TextFieldData{Value: "value", Inset: 1}))
	if got, want := ansi.StringWidth(line), frame.InnerWidth()-1; got != want {
		t.Fatalf("child input width = %d, want %d", got, want)
	}

	narrowFrame := NewFrame(18, 14).WithInnerWidth(0)
	line = ansi.Strip(RenderTextField(narrowFrame, TextFieldData{
		Value:  "very long editable value",
		Inset:  1,
		Active: true,
	}))
	if strings.Contains(line, "…") {
		t.Fatalf("text field should truncate without ellipsis: %q", line)
	}
}

func assertContainsAll(t *testing.T, view string, values ...string) {
	t.Helper()
	for _, want := range values {
		if !strings.Contains(view, want) {
			t.Fatalf("rendered view missing %q:\n%s", want, view)
		}
	}
}

func TestRenderListTruncatesWhenViewportIsNarrow(t *testing.T) {
	frame := NewFrame(34, 14)
	view := ansi.Strip(RenderPickerCard(frame, PickerData{
		Title: "Very Long Configuration Screen Title",
		Meta:  "Step 123456789",
		Items: []Item{
			{Title: "Very Long Provider Display Name", Status: "Very Long Model Value"},
		},
		Help: "enter select and do something very long",
	}))

	if !strings.Contains(view, "…") {
		t.Fatalf("rendered list should contain ellipsis when narrow:\n%s", view)
	}
	assertMaxLineWidth(t, view, 34)
}

func findLine(lines []string, needle string) string {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func assertLineOrder(t *testing.T, lines []string, first string, second string) {
	t.Helper()
	firstIndex := lineIndex(lines, first)
	secondIndex := lineIndex(lines, second)
	if firstIndex < 0 || secondIndex < 0 {
		t.Fatalf("missing %q or %q:\n%s", first, second, strings.Join(lines, "\n"))
	}
	if firstIndex >= secondIndex {
		t.Fatalf("%q should render before %q:\n%s", first, second, strings.Join(lines, "\n"))
	}
}

func lineIndex(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func assertMaxLineWidth(t *testing.T, view string, width int) {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line width = %d, want <= %d for %q\n%s", got, width, line, view)
		}
	}
}
