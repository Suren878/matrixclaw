package editor

import (
	"math/rand"
	"os"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const (
	defaultTextareaWidth = 40
	editorPromptWidth    = 2
)

var readyPlaceholders = [...]string{
	"Awaiting input...",
}

var workingPlaceholders = [...]string{
	"Working...",
}

type Model struct {
	com                *common.Common
	textarea           textarea.Model
	attachments        *Attachments
	width              int
	working            bool
	readyPlaceholder   string
	workingPlaceholder string
}

func New(com *common.Common, keyMap model.KeyMap) Model {
	if com == nil {
		com = common.DefaultCommon()
	}

	ta := textarea.New()
	applyTextAreaStyles(&ta, com.Styles.TextArea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.MaxHeight = TextareaMaxHeight
	ta.SetHeight(TextareaMinHeight)
	ta.Focus()

	m := Model{
		com:      com,
		textarea: ta,
		attachments: NewAttachments(
			NewRenderer(
				com.Styles.Attachments.Normal,
				com.Styles.Attachments.Deleting,
				com.Styles.Attachments.Image,
				com.Styles.Attachments.Text,
			),
			Keymap{
				DeleteMode: keyMap.Editor.AttachmentDeleteMode,
				DeleteAll:  keyMap.Editor.DeleteAllAttachments,
				Escape:     keyMap.Editor.Escape,
			},
		),
		width: defaultTextareaWidth,
	}

	m.setEditorPrompt()
	m.textarea.SetWidth(m.width)
	m.RandomizePlaceholders()
	m.textarea.Placeholder = m.readyPlaceholder
	m.syncHeight()

	return m
}

func (m *Model) Focus() tea.Cmd {
	cmd := m.textarea.Focus()
	m.setEditorPrompt()
	return cmd
}

func (m *Model) Blur() {
	m.textarea.Blur()
	m.setEditorPrompt()
}

func (m *Model) Focused() bool {
	return m.textarea.Focused()
}

func (m *Model) Value() string {
	return m.textarea.Value()
}

func (m *Model) Length() int {
	return m.textarea.Length()
}

func (m *Model) Height() int {
	return m.textarea.Height()
}

func (m *Model) EditorHeight() int {
	return m.Height() + EditorHeightMargin
}

func (m *Model) Line() int {
	return m.textarea.Line()
}

func (m *Model) Column() int {
	return m.textarea.LineInfo().CharOffset
}

func (m *Model) LineCount() int {
	return m.textarea.LineCount()
}

func (m *Model) SetWidth(width int) tea.Cmd {
	if width <= 0 {
		return nil
	}
	prevHeight := m.Height()
	m.width = width
	m.textarea.SetWidth(width)
	m.syncHeight()
	return m.handleHeightChange(prevHeight)
}

func (m *Model) SetValue(value string) {
	m.textarea.SetValue(value)
	m.syncHeight()
}

func (m *Model) InsertString(value string) {
	m.textarea.InsertString(value)
	m.syncHeight()
}

func (m *Model) InsertRune(r rune) {
	m.textarea.InsertRune(r)
	m.syncHeight()
}

func (m *Model) Reset() {
	m.textarea.Reset()
	m.syncHeight()
}

func (m *Model) MoveToBegin() {
	for m.textarea.Line() > 0 {
		m.textarea.CursorUp()
	}
	m.textarea.CursorStart()
}

func (m *Model) MoveToEnd() {
	last := max(m.textarea.LineCount()-1, 0)
	for m.textarea.Line() < last {
		m.textarea.CursorDown()
	}
	m.textarea.CursorEnd()
}

func (m *Model) CursorStart() {
	m.textarea.CursorStart()
}

func (m *Model) IsAtStart() bool {
	return m.textarea.Line() == 0 && m.textarea.LineInfo().ColumnOffset == 0
}

func (m *Model) IsAtEnd() bool {
	lineCount := m.textarea.LineCount()
	if lineCount == 0 {
		return true
	}
	if m.textarea.Line() != lineCount-1 {
		return false
	}
	info := m.textarea.LineInfo()
	return info.CharOffset >= info.CharWidth-1 || info.CharWidth == 0
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	return m.UpdateWithPrevHeight(msg, m.Height())
}

func (m *Model) UpdateWithPrevHeight(msg tea.Msg, prevHeight int) tea.Cmd {
	ta, cmd := m.textarea.Update(msg)
	m.textarea = ta
	m.syncHeight()
	return tea.Batch(cmd, m.handleHeightChange(prevHeight))
}

func (m *Model) UpdateAttachments(msg tea.Msg) bool {
	return m.attachments.Update(msg)
}

func (m *Model) Attachments() []Attachment {
	return m.attachments.List()
}

func (m *Model) ResetAttachments() {
	m.attachments.Reset()
}

func (m *Model) SetWorking(working bool) {
	m.working = working
	if working {
		m.textarea.Placeholder = m.workingPlaceholder
		return
	}
	m.textarea.Placeholder = m.readyPlaceholder
}

func (m *Model) RandomizePlaceholders() {
	m.workingPlaceholder = workingPlaceholders[rand.Intn(len(workingPlaceholders))]
	m.readyPlaceholder = readyPlaceholders[rand.Intn(len(readyPlaceholders))]
	if m.working {
		m.textarea.Placeholder = m.workingPlaceholder
		return
	}
	m.textarea.Placeholder = m.readyPlaceholder
}

func (m *Model) Render(width int) string {
	if width > 0 && width != m.width {
		_ = m.SetWidth(width)
	}

	parts := make([]string, 0, 2)
	if len(m.attachments.List()) > 0 {
		parts = append(parts, m.attachments.Render(width))
	}
	parts = append(parts, m.textarea.View())
	return strings.Join(parts, "\n")
}

func (m *Model) OpenExternalEditor() tea.Cmd {
	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return msgCmd(ExternalEditorErrorMsg{Err: err})
	}

	tmpPath := tmpfile.Name()
	defer tmpfile.Close() //nolint:errcheck
	if _, err := tmpfile.WriteString(m.Value()); err != nil {
		return msgCmd(ExternalEditorErrorMsg{Err: err})
	}

	cmd, err := externalEditorCommand(nil, "matrixclaw", tmpPath, editorAtPosition(m.Line()+1, m.Column()+1))
	if err != nil {
		return msgCmd(ExternalEditorErrorMsg{Err: err})
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err != nil {
			return ExternalEditorErrorMsg{Err: err}
		}

		content, err := os.ReadFile(tmpPath)
		if err != nil {
			return ExternalEditorErrorMsg{Err: err}
		}
		if len(content) == 0 {
			return ExternalEditorWarningMsg{Message: "Message is empty"}
		}
		return OpenEditorMsg{Text: strings.TrimSpace(string(content))}
	})
}

func (m *Model) setEditorPrompt() {
	m.textarea.SetPromptFunc(editorPromptWidth, func(info textarea.PromptInfo) string {
		return m.normalPromptFunc(info.LineNumber)
	})
}

func (m *Model) normalPromptFunc(lineIdx int) string {
	t := m.com.Styles
	if lineIdx == 0 {
		if m.textarea.Focused() {
			return t.EditorPromptNormalFocused.Width(editorPromptWidth).Render(">")
		}
		return t.EditorPromptNormalBlurred.Width(editorPromptWidth).Render("○")
	}
	return strings.Repeat(" ", editorPromptWidth)
}

func (m *Model) handleHeightChange(prevHeight int) tea.Cmd {
	if m.Height() == prevHeight {
		return nil
	}
	return msgCmd(HeightChangedMsg{
		PreviousTextareaHeight: prevHeight,
		TextareaHeight:         m.Height(),
		PreviousEditorHeight:   prevHeight + EditorHeightMargin,
		EditorHeight:           m.EditorHeight(),
	})
}

func (m *Model) syncHeight() {
	lines := estimateWrappedLines(m.Value(), m.contentWidth())
	m.textarea.SetHeight(clamp(lines, TextareaMinHeight, TextareaMaxHeight))
}

func (m *Model) contentWidth() int {
	width := m.width
	if width <= 0 {
		width = defaultTextareaWidth
	}

	styles := m.textarea.Styles()
	baseStyle := styles.Blurred.Base
	if m.textarea.Focused() {
		baseStyle = styles.Focused.Base
	}

	contentWidth := width - baseStyle.GetHorizontalFrameSize() - editorPromptWidth
	if contentWidth < 1 {
		return 1
	}
	return contentWidth
}

func applyTextAreaStyles(ta *textarea.Model, styles surfacestyles.TextAreaStyles) {
	next := textarea.DefaultDarkStyles()
	next.Focused = textarea.StyleState{
		Base:             styles.Focused.Base,
		Text:             styles.Focused.Text,
		LineNumber:       styles.Focused.LineNumber,
		CursorLine:       styles.Focused.CursorLine,
		CursorLineNumber: styles.Focused.CursorLineNumber,
		Placeholder:      styles.Focused.Placeholder,
		Prompt:           styles.Focused.Prompt,
	}
	next.Blurred = textarea.StyleState{
		Base:             styles.Blurred.Base,
		Text:             styles.Blurred.Text,
		LineNumber:       styles.Blurred.LineNumber,
		CursorLine:       styles.Blurred.CursorLine,
		CursorLineNumber: styles.Blurred.CursorLineNumber,
		Placeholder:      styles.Blurred.Placeholder,
		Prompt:           styles.Blurred.Prompt,
	}
	next.Cursor.Color = styles.Cursor.Color
	next.Cursor.Blink = styles.Cursor.Blink
	ta.SetStyles(next)
}

func estimateWrappedLines(value string, width int) int {
	if width <= 0 {
		return TextareaMinHeight
	}

	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return TextareaMinHeight
	}

	total := 0
	for _, line := range lines {
		lineWidth := ansi.StringWidth(line)
		if lineWidth == 0 {
			total++
			continue
		}
		total += max(1, (lineWidth+width-1)/width)
	}

	return total
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func msgCmd(msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg
	}
}
