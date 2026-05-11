package styles

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestDefaultStylesMarkdownAccentColors(t *testing.T) {
	sty := DefaultStyles()

	cases := []struct {
		name string
		got  *string
		want string
	}{
		{name: "code", got: sty.Markdown.Code.Color, want: hex(sty.Green)},
		{name: "link text", got: sty.Markdown.LinkText.Color, want: hex(sty.Green)},
	}
	for _, tt := range cases {
		if tt.got == nil {
			t.Fatalf("%s color is nil", tt.name)
		}
		if *tt.got != tt.want {
			t.Fatalf("%s color = %q, want %q", tt.name, *tt.got, tt.want)
		}
	}
}

func TestDefaultStylesSemanticForegrounds(t *testing.T) {
	sty := DefaultStyles()

	cases := []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{name: "files path", got: sty.Files.Path.GetForeground(), want: gloss(sty.FgMuted)},
		{name: "tool content line", got: sty.Tool.ContentLine.GetForeground(), want: gloss(sty.FgMuted)},
		{name: "tool param", got: sty.Tool.ParamMain.GetForeground(), want: gloss(sty.FgBase)},
		{name: "muted", got: sty.Muted.GetForeground(), want: gloss(lipgloss.Color("#949594"))},
		{name: "half muted", got: sty.HalfMuted.GetForeground(), want: gloss(lipgloss.Color("#949594"))},
	}
	for _, tt := range cases {
		if tt.got != tt.want {
			t.Fatalf("%s foreground = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestDefaultStylesUserMessageBackgroundUsesRequestedGray(t *testing.T) {
	sty := DefaultStyles()
	want := gloss(lipgloss.Color(semanticUserMessageBg))

	if got := sty.Chat.Message.FocusedLine.GetBackground(); got != want {
		t.Fatalf("user message background = %q, want %q", got, want)
	}
	if got := sty.Chat.Message.UserMarker.GetBackground(); got != want {
		t.Fatalf("user marker background = %q, want %q", got, want)
	}
	if got := sty.Dialog.ContentPanel.GetBackground(); got != want {
		t.Fatalf("dialog content panel background = %q, want %q", got, want)
	}
	if got, want := sty.TextArea.Focused.CursorLine.GetBackground(), lipgloss.NewStyle().GetBackground(); got != want {
		t.Fatalf("focused input line background = %q, want %q", got, want)
	}
}

func TestDefaultStylesToolAndChatEmphasis(t *testing.T) {
	sty := DefaultStyles()

	if got, want := sty.Tool.NameNormal.GetForeground(), gloss(sty.White); got != want {
		t.Fatalf("tool name foreground = %q, want %q", got, want)
	}
	if !sty.Tool.NameNormal.GetBold() {
		t.Fatal("tool name should be bold")
	}
	if sty.Tool.NameNormal.GetUnderline() {
		t.Fatal("tool name should not be underlined")
	}
	if got, want := sty.Tool.JobAction.GetForeground(), gloss(sty.White); got != want {
		t.Fatalf("job action foreground = %q, want %q", got, want)
	}
	if !sty.Tool.JobAction.GetBold() {
		t.Fatal("job action should be bold")
	}

	cases := []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{name: "tool marker", got: sty.Chat.Message.ToolMarker.GetForeground(), want: gloss(sty.White)},
		{name: "user blurred border", got: sty.Chat.Message.UserBlurred.GetBorderLeftForeground(), want: gloss(sty.Primary)},
		{name: "user focused border", got: sty.Chat.Message.UserFocused.GetBorderLeftForeground(), want: gloss(sty.White)},
		{name: "assistant focused border", got: sty.Chat.Message.AssistantFocused.GetBorderLeftForeground(), want: gloss(sty.White)},
		{name: "tool focused border", got: sty.Chat.Message.ToolCallFocused.GetBorderLeftForeground(), want: gloss(sty.White)},
	}
	for _, tt := range cases {
		if tt.got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestDefaultStylesControlsUseInitialSelectionColors(t *testing.T) {
	sty := DefaultStyles()
	buttonBg := gloss(lipgloss.Color(semanticControlSelected))

	if got := sty.ButtonFocus.GetBackground(); got != buttonBg {
		t.Fatalf("button focus background = %q, want %q", got, buttonBg)
	}
	selectedFg := gloss(lipgloss.Color(semanticControlText))
	if got := sty.ButtonFocus.GetForeground(); got != selectedFg {
		t.Fatalf("button focus foreground = %q, want %q", got, selectedFg)
	}
	if got := sty.ButtonBlur.GetBackground(); got != gloss(sty.BgSubtle) {
		t.Fatalf("button blur background = %q, want %q", got, gloss(sty.BgSubtle))
	}
	if got := sty.Dialog.SelectedItem.GetBackground(); got != buttonBg {
		t.Fatalf("dialog selected item background = %q, want %q", got, buttonBg)
	}
	if got := sty.Dialog.SelectedItem.GetForeground(); got != selectedFg {
		t.Fatalf("dialog selected item foreground = %q, want %q", got, selectedFg)
	}
	if !sty.Dialog.SelectedItem.GetBold() {
		t.Fatal("dialog selected item should be bold")
	}
}

func TestDefaultStylesDiffLinesUseReadableSemanticTextColors(t *testing.T) {
	lipgloss.Writer.Profile = colorprofile.TrueColor
	sty := DefaultStyles()

	if got := sty.Diff.DeleteLine.Code.GetForeground(); got == sty.Base.GetForeground() {
		t.Fatalf("delete diff code foreground should not use base text color: %q", got)
	}
	if got := sty.Diff.InsertLine.Code.GetForeground(); got == sty.Base.GetForeground() {
		t.Fatalf("insert diff code foreground should not use base text color: %q", got)
	}
	if got := sty.Diff.DeleteLine.Code.GetBackground(); got == sty.Diff.InsertLine.Code.GetBackground() {
		t.Fatalf("delete and insert diff backgrounds should differ: %q", got)
	}
	if !isVisiblyRed(sty.Diff.DeleteLine.Code.GetBackground()) {
		t.Fatalf("delete diff background should be visibly red, got %q", sty.Diff.DeleteLine.Code.GetBackground())
	}
}

func isVisiblyRed(c color.Color) bool {
	if c == nil {
		return false
	}
	r, g, b, _ := c.RGBA()
	return r>>8 >= 80 && r > g*2 && r > b*2
}
