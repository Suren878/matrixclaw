package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestUserMessageItemRenderContainsText(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-user",
		Role: surfacemessage.User,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "hello from user"},
		},
	}

	rendered := NewUserMessageItem(&styles, &message).Render(80)
	plain := xansi.Strip(rendered)
	plain = strings.ReplaceAll(plain, "│", " ")
	plain = strings.Join(strings.Fields(plain), " ")
	if !strings.Contains(plain, "hello from user") {
		t.Fatalf("rendered user message missing text: %q", plain)
	}
}

func TestUserMessageUsesUnifiedMarkerWithoutBorder(t *testing.T) {
	lipgloss.Writer.Profile = colorprofile.TrueColor

	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-user-focused",
		Role: surfacemessage.User,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "focused row"},
		},
	}
	item := NewUserMessageItem(&styles, &message)

	rendered := item.Render(40)
	plain := xansi.Strip(rendered)
	if strings.Contains(plain, "▌") || strings.Contains(plain, "│") {
		t.Fatalf("user message should not render border bars, got %q", plain)
	}
	if !strings.Contains(plain, "> focused row") {
		t.Fatalf("expected user message marker, got %q", plain)
	}
	plainLines := strings.Split(plain, "\n")
	if len(plainLines) != 3 {
		t.Fatalf("expected user message block with top/body/bottom rows, got %q", plain)
	}
	for i, line := range plainLines {
		if got := xansi.StringWidth(line); got != 40 {
			t.Fatalf("user message line %d width = %d, want 40: %q", i, got, line)
		}
	}

	buf := uv.NewScreenBuffer(40, 3)
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())
	for y := 0; y < 3; y++ {
		for x := 0; x < 40; x++ {
			cell := buf.CellAt(x, y)
			if cell == nil || cell.Style.Bg == nil {
				t.Fatalf("user background missing at cell %d,%d in %q", x, y, rendered)
			}
		}
	}
}

func TestUserMessageBackgroundSurvivesInnerANSI(t *testing.T) {
	lipgloss.Writer.Profile = colorprofile.TrueColor

	styles := surfacestyles.DefaultStyles()
	rendered := renderUserMessageLines(&styles, "\x1b[30;40mblack ansi text\x1b[0m", false, 30)

	buf := uv.NewScreenBuffer(30, 3)
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())
	for y := 0; y < 3; y++ {
		for x := 0; x < 30; x++ {
			cell := buf.CellAt(x, y)
			if cell == nil || cell.Style.Bg == nil {
				t.Fatalf("user background missing at cell %d,%d in %q", x, y, rendered)
			}
		}
	}
	if !strings.Contains(xansi.Strip(rendered), "black ansi text") {
		t.Fatalf("rendered text missing after ANSI normalization, got %q", rendered)
	}
}

func TestFocusedUserMessageHidesMarkerGlyph(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-user-focused",
		Role: surfacemessage.User,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "focused row"},
		},
	}
	item := NewUserMessageItem(&styles, &message)
	item.(interface{ SetFocused(bool) }).SetFocused(true)

	plain := xansi.Strip(item.Render(40))
	if strings.Contains(plain, "> focused row") {
		t.Fatalf("expected focused user marker glyph to be hidden, got %q", plain)
	}
	if !strings.Contains(plain, "  focused row") {
		t.Fatalf("expected focused user text alignment to remain, got %q", plain)
	}
}

func TestUserMessageItemUsesLegacyMarkdownPath(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-user-markdown",
		Role: surfacemessage.User,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "список:\n- filename"},
		},
	}

	rendered := NewUserMessageItem(&styles, &message).Render(80)
	if strings.Contains(rendered, "\x1b[38;2;0;255;178mfilename\x1b[0m") {
		t.Fatalf("expected user markdown to avoid assistant-only file token renderer, got %q", rendered)
	}
}

func TestAssistantMessageItemRenderContainsText(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-assistant",
		Role: surfacemessage.Assistant,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "hello from assistant"},
		},
	}

	rendered := NewAssistantMessageItem(&styles, &message).Render(80)
	plain := xansi.Strip(rendered)
	if !strings.Contains(plain, "hello from assistant") {
		t.Fatalf("rendered assistant message missing text: %q", plain)
	}
}

func TestAssistantErrorItemEnterOpensErrorPreview(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-assistant-error",
		Role: surfacemessage.Assistant,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.Finish{
				Reason:  surfacemessage.FinishReasonError,
				Message: "provider request failed",
				Details: "openai: unsupported parameter reasoning_effort for this request",
			},
		},
	}

	item := NewAssistantMessageItem(&styles, &message)
	handler, ok := item.(KeyEventHandler)
	if !ok {
		t.Fatalf("item does not implement KeyEventHandler: %T", item)
	}
	handled, cmd := handler.HandleKeyEvent(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected enter to open assistant error preview, handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	action, ok := msg.(surfacedialog.ActionOpenFilePreview)
	if !ok {
		t.Fatalf("msg = %T, want ActionOpenFilePreview", msg)
	}
	for _, want := range []string{"provider request failed", "unsupported parameter reasoning_effort"} {
		if !strings.Contains(action.Data.Content, want) {
			t.Fatalf("expected %q in error preview content, got %q", want, action.Data.Content)
		}
	}
}

func TestAssistantMessageItemRenderWithZeroWidthDoesNotHang(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-assistant-zero-width",
		Role: surfacemessage.Assistant,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "список:\n- filename"},
		},
	}

	done := make(chan string, 1)
	go func() {
		done <- NewAssistantMessageItem(&styles, &message).Render(0)
	}()

	select {
	case rendered := <-done:
		if !strings.Contains(xansi.Strip(rendered), "filename") {
			t.Fatalf("rendered zero-width assistant message missing text: %q", rendered)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("zero-width assistant render hung")
	}
}

func TestAssistantMessageStartsWithMarkerAndIndentedText(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	rendered := renderUnifiedMessageLines(&styles, "first line\nsecond line", false, "•", styles.Chat.Message.AssistantMarker)
	plainLines := strings.Split(xansi.Strip(rendered), "\n")
	if len(plainLines) < 2 {
		t.Fatalf("expected multiline assistant render, got %q", rendered)
	}
	if !strings.Contains(plainLines[0], "• first line") {
		t.Fatalf("expected first assistant line to start with marker, got %q", plainLines[0])
	}
	if strings.Contains(plainLines[1], "•") {
		t.Fatalf("expected only first content line to contain marker, got %q", plainLines[1])
	}
	if !strings.Contains(plainLines[1], "  second line") {
		t.Fatalf("expected second assistant line to keep two-char indent, got %q", plainLines[1])
	}
}

func TestUnifiedMessageMarkerHasBackground(t *testing.T) {
	lipgloss.Writer.Profile = colorprofile.TrueColor

	styles := surfacestyles.DefaultStyles()
	rendered := renderUnifiedMessageLines(&styles, "selected", true, "•", styles.Chat.Message.ToolMarker)
	plain := xansi.Strip(rendered)
	if strings.Contains(plain, "│") || strings.Contains(plain, "▌") {
		t.Fatalf("expected message to avoid border glyphs, got %q", plain)
	}
	if strings.HasPrefix(plain, "•") {
		t.Fatalf("expected focused marker glyph to be hidden, got %q", plain)
	}
	if !strings.HasPrefix(plain, "  selected") {
		t.Fatalf("expected focused message text alignment to remain, got %q", plain)
	}

	buf := uv.NewScreenBuffer(12, 1)
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())
	cell := buf.CellAt(0, 0)
	if cell == nil || cell.Style.Bg == nil {
		t.Fatalf("expected marker cell background in %q", rendered)
	}
}

func TestAssistantMessageItemUsesSharedMarkdownRenderer(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-assistant-markdown",
		Role: surfacemessage.Assistant,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "список:\n- filename"},
		},
	}

	rendered := NewAssistantMessageItem(&styles, &message).Render(80)
	plain := xansi.Strip(rendered)
	if strings.Contains(plain, "• filename") {
		t.Fatalf("expected markdown list bullet to avoid round marker, got %q", plain)
	}
	if !strings.Contains(plain, "filename") {
		t.Fatalf("expected assistant markdown content to remain visible, got %q", plain)
	}
}

func TestNormalizeAssistantMarkdownANSIRemovesBlueBackground(t *testing.T) {
	rendered := normalizeAssistantMarkdownANSI("a \x1b[44;3mfilename\x1b[0m b")
	if strings.Contains(rendered, "\x1b[44;3m") {
		t.Fatalf("expected assistant inline code to avoid blue background, got %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[36mfilename\x1b[0m") {
		t.Fatalf("expected assistant inline code to preserve colored text, got %q", rendered)
	}
}

func TestNormalizeAssistantMarkdownANSIUsesDashBullets(t *testing.T) {
	rendered := normalizeAssistantMarkdownANSI("\x1b[32m• \x1b[0mitem")
	if strings.Contains(rendered, "\x1b[32m• \x1b[0m") {
		t.Fatalf("expected assistant bullet to avoid green marker, got %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[97m- \x1b[0mitem") {
		t.Fatalf("expected assistant bullet to use dash marker, got %q", rendered)
	}
}
