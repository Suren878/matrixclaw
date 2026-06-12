package dialog

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const TextEditCommandID = "text_edit_command"

type TextEditCommandData = controlplane.TextEditData

type TextEditCommand struct {
	data      TextEditCommandData
	input     textarea.Model
	loading   bool
	frame     int
	lastWidth int
	lastY     int
}

func NewTextEditCommand(_ *surfacecommon.Common, data TextEditCommandData) *TextEditCommand {
	input := textarea.New()
	input.Prompt = ""
	input.Placeholder = strings.TrimSpace(data.Placeholder)
	input.ShowLineNumbers = false
	input.SetValue(data.Value)
	input.Focus()
	return &TextEditCommand{data: data, input: input}
}

func (*TextEditCommand) ID() string { return TextEditCommandID }

func (d *TextEditCommand) HandleMsg(msg tea.Msg) Action {
	if _, ok := msg.(loadingTickMsg); ok {
		if !d.loading {
			return nil
		}
		d.frame = (d.frame + 1) % len(loadingFrames)
		return ActionCmd{Cmd: loadingTickCmd()}
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch keyMsg.String() {
		case "esc", "alt+esc":
			if command := strings.TrimSpace(d.data.CancelCommand); command != "" {
				return ActionRunControlplaneCommand{Command: command}
			}
			return ActionClose{}
		case "enter", "ctrl+s":
			return ActionRunControlplaneCommand{Command: d.data.SubmitCommandPrefix + d.input.Value()}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *TextEditCommand) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	frame := components.NewFrame(area.Dx(), area.Dy()).WithInnerWidth(0)
	d.input.SetWidth(max(1, frame.InnerWidth()))
	d.input.SetHeight(components.TextViewEditorHeight(frame))
	d.lastWidth = frame.InnerWidth()
	d.lastY = 4
	view := components.RenderTextViewCard(frame, components.TextViewData{
		Title: loadingTitle(d.data.Title, d.loading, d.frame),
		Text:  d.input.View(),
	})
	DrawCenter(scr, area, view)
	return nil
}

func (d *TextEditCommand) StartLoading() tea.Cmd {
	d.loading = true
	d.frame = 0
	return loadingTickCmd()
}

func (d *TextEditCommand) StopLoading() {
	d.loading = false
	d.frame = 0
}
