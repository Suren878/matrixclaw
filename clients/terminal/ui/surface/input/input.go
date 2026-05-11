package input

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	surfacemodel "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
)

type SubmitMsg struct {
	Content     string
	Attachments []surfaceeditor.Attachment
}

type FocusMainMsg struct{}

type NewSessionMsg struct{}

type OpenCommandsMsg struct{}

type AddImageMsg struct{}

type PasteImageMsg struct{}

type QuitRequestMsg struct{}

type Model struct {
	editor  surfaceeditor.Model
	keyMap  surfacemodel.KeyMap
	history surfacemodel.PromptHistory
}

func New(com *common.Common) Model {
	keyMap := surfacemodel.DefaultKeyMap()
	return Model{
		editor:  surfaceeditor.New(com, keyMap),
		keyMap:  keyMap,
		history: surfacemodel.NewPromptHistory(),
	}
}

func (m *Model) Editor() *surfaceeditor.Model {
	return &m.editor
}

func (m *Model) KeyMap() surfacemodel.KeyMap {
	return m.keyMap
}

func (m *Model) SetPromptHistory(messages []string) {
	m.history.SetMessages(messages)
}

func (m *Model) PromptHistory() surfacemodel.PromptHistory {
	return m.history
}

func (m *Model) SetWidth(width int) tea.Cmd {
	return m.editor.SetWidth(width)
}

func (m *Model) SetWorking(working bool) {
	m.editor.SetWorking(working)
}

func (m *Model) Focus() tea.Cmd {
	return m.editor.Focus()
}

func (m *Model) Blur() {
	m.editor.Blur()
}

func (m *Model) Focused() bool {
	return m.editor.Focused()
}

func (m *Model) Value() string {
	return m.editor.Value()
}

func (m *Model) Render(width int) string {
	return m.editor.Render(width)
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case surfaceeditor.OpenEditorMsg:
		prevHeight := m.editor.Height()
		m.editor.SetValue(msg.Text)
		m.editor.MoveToEnd()
		return m.editor.UpdateWithPrevHeight(nil, prevHeight)

	case tea.KeyPressMsg:
		if ok := m.editor.UpdateAttachments(msg); ok {
			return nil
		}

		switch {
		case key.Matches(msg, m.keyMap.Editor.AddImage):
			return msgCmd(AddImageMsg{})

		case key.Matches(msg, m.keyMap.Editor.PasteImage):
			return msgCmd(PasteImageMsg{})

		case key.Matches(msg, m.keyMap.Editor.SendMessage):
			return m.handleSendMessage()

		case key.Matches(msg, m.keyMap.Chat.NewSession):
			return msgCmd(NewSessionMsg{})

		case key.Matches(msg, m.keyMap.Tab):
			m.editor.Blur()
			return msgCmd(FocusMainMsg{})

		case key.Matches(msg, m.keyMap.Editor.OpenEditor):
			return m.editor.OpenExternalEditor()

		case key.Matches(msg, m.keyMap.Editor.Newline):
			prevHeight := m.editor.Height()
			m.editor.InsertRune('\n')
			return m.editor.UpdateWithPrevHeight(nil, prevHeight)

		case key.Matches(msg, m.keyMap.Editor.HistoryPrev):
			return m.handleHistoryUp(msg)

		case key.Matches(msg, m.keyMap.Editor.HistoryNext):
			return m.handleHistoryDown(msg)

		case key.Matches(msg, m.keyMap.Editor.Escape):
			return m.handleHistoryEscape(msg)

		case key.Matches(msg, m.keyMap.Editor.Commands) && m.editor.Value() == "":
			return msgCmd(OpenCommandsMsg{})

		default:
			prevHeight := m.editor.Height()
			oldValue := m.editor.Value()
			cmd := m.editor.UpdateWithPrevHeight(msg, prevHeight)
			m.history.UpdateDraft(oldValue, m.editor.Value())
			return cmd
		}
	}

	if ok := m.editor.UpdateAttachments(msg); ok {
		return nil
	}

	return m.editor.Update(msg)
}

func (m *Model) handleSendMessage() tea.Cmd {
	prevHeight := m.editor.Height()
	value := m.editor.Value()
	if before, ok := strings.CutSuffix(value, "\\"); ok {
		m.editor.SetValue(before)
		return m.editor.UpdateWithPrevHeight(nil, prevHeight)
	}

	m.editor.Reset()
	resizeCmd := m.editor.UpdateWithPrevHeight(nil, prevHeight)

	value = strings.TrimSpace(value)
	if value == "exit" || value == "quit" {
		return tea.Batch(resizeCmd, msgCmd(QuitRequestMsg{}))
	}

	attachments := m.editor.Attachments()
	m.editor.ResetAttachments()
	if len(value) == 0 && !surfaceeditor.ContainsMessageAttachment(attachments) {
		for _, attachment := range attachments {
			_ = m.editor.UpdateAttachments(attachment)
		}
		return resizeCmd
	}

	m.editor.RandomizePlaceholders()
	m.history.Reset()

	return tea.Batch(
		resizeCmd,
		msgCmd(SubmitMsg{
			Content:     value,
			Attachments: attachments,
		}),
	)
}

func (m *Model) handleHistoryUp(msg tea.Msg) tea.Cmd {
	prevHeight := m.editor.Height()
	if m.editor.Length() == 0 || m.editor.IsAtStart() {
		if next, ok := m.history.Prev(m.editor.Value()); ok {
			m.editor.SetValue(next)
			m.editor.MoveToBegin()
			return m.editor.UpdateWithPrevHeight(nil, prevHeight)
		}
	}

	if m.editor.Line() == 0 {
		m.editor.CursorStart()
		return nil
	}

	return m.editor.Update(msg)
}

func (m *Model) handleHistoryDown(msg tea.Msg) tea.Cmd {
	prevHeight := m.editor.Height()
	if m.editor.IsAtEnd() {
		if next, ok := m.history.Next(); ok {
			m.editor.SetValue(next)
			return m.editor.UpdateWithPrevHeight(nil, prevHeight)
		}
	}

	if m.editor.Line() == max(m.editor.LineCount()-1, 0) {
		m.editor.MoveToEnd()
		return m.editor.Update(nil)
	}

	return m.editor.Update(msg)
}

func (m *Model) handleHistoryEscape(msg tea.Msg) tea.Cmd {
	prevHeight := m.editor.Height()
	if draft, ok := m.history.RestoreDraft(); ok {
		m.editor.SetValue(draft)
		return m.editor.UpdateWithPrevHeight(nil, prevHeight)
	}

	return m.editor.Update(msg)
}

func msgCmd(msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg
	}
}
