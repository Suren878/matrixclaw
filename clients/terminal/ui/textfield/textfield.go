package textfield

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/theme"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const DefaultCharLimit = 4096

type Model struct {
	input textinput.Model
}

type Option func(*Model)

func New(placeholder string, value string, options ...Option) Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = DefaultCharLimit
	input.SetWidth(64)
	input.SetValue(value)
	input.CursorEnd()
	applyDefaultStyles(&input)
	_ = input.Focus()

	model := Model{input: input}
	for _, option := range options {
		if option != nil {
			option(&model)
		}
	}
	return model
}

func WithCharLimit(limit int) Option {
	return func(m *Model) {
		m.input.CharLimit = limit
	}
}

func WithWidth(width int) Option {
	return func(m *Model) {
		m.input.SetWidth(width)
	}
}

func WithSecret(secret bool) Option {
	return func(m *Model) {
		if !secret {
			m.input.EchoMode = textinput.EchoNormal
			return
		}
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
	}
}

func WithSurfaceStyles(styles surfacestyles.TextInputStyles) Option {
	return func(m *Model) {
		next := textinput.DefaultDarkStyles()
		next.Focused.Text = styles.Focused.Text
		next.Focused.Placeholder = styles.Focused.Placeholder
		next.Focused.Prompt = styles.Focused.Prompt
		next.Focused.Suggestion = styles.Focused.Suggestion
		next.Blurred.Text = styles.Blurred.Text
		next.Blurred.Placeholder = styles.Blurred.Placeholder
		next.Blurred.Prompt = styles.Blurred.Prompt
		next.Blurred.Suggestion = styles.Blurred.Suggestion
		next.Cursor.Color = styles.Cursor.Color
		next.Cursor.Blink = styles.Cursor.Blink
		m.input.SetStyles(next)
	}
}

func (m Model) Value() string {
	return m.input.Value()
}

func (m Model) Placeholder() string {
	return m.input.Placeholder
}

func (m Model) View() string {
	return m.input.View()
}

func (m Model) Reset(value string) Model {
	m.input.SetValue(value)
	m.input.CursorEnd()
	return m
}

func (m *Model) SetWidth(width int) {
	m.input.SetWidth(width)
}

func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m Model) Cursor(styles surfacestyles.TextInputStyles) *uv.Cursor {
	if !m.input.Focused() {
		return nil
	}

	x := lipgloss.Width(m.input.Prompt) + cursorX(m.input)
	cur := uv.NewCursor(x, 0)
	cur.Color = styles.Cursor.Color
	cur.Shape = uv.CursorBlock
	cur.Blink = styles.Cursor.Blink
	return cur
}

func applyDefaultStyles(input *textinput.Model) {
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	styles.Blurred = styles.Focused
	styles.Cursor.Color = lipgloss.Color(theme.Head)
	input.SetStyles(styles)
}

func cursorX(input textinput.Model) int {
	pos := input.Position()
	runes := []rune(input.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	prefix := string(runes[:pos])
	inputWidth := input.Width()
	if inputWidth <= 0 {
		return ansi.StringWidth(prefix)
	}

	width := ansi.StringWidth(prefix)
	if width <= inputWidth {
		return width
	}

	visibleWidth := 0
	for i := len(runes[:pos]) - 1; i >= 0; i-- {
		runeWidth := ansi.StringWidth(string(runes[i]))
		if visibleWidth+runeWidth > inputWidth {
			break
		}
		visibleWidth += runeWidth
	}
	return visibleWidth
}
