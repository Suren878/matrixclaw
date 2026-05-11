package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const PromptCommandID = "prompt_command"

type PromptCommandData = controlplane.PromptData

type PromptCommand struct {
	com   *surfacecommon.Common
	input textinput.Model
	data  PromptCommandData

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
}

func NewPromptCommand(com *surfacecommon.Common, data PromptCommandData) *PromptCommand {
	if com == nil {
		com = surfacecommon.DefaultCommon()
	}
	input := textinput.New()
	input.Placeholder = strings.TrimSpace(data.Placeholder)
	input.SetValue(strings.TrimSpace(data.Value))
	input.CursorEnd()
	if data.Sensitive {
		input.EchoMode = textinput.EchoPassword
	}
	applyTextInputStyles(&input, com.Styles.TextInput)
	_ = input.Focus()

	d := &PromptCommand{
		com:   com,
		input: input,
		data:  data,
	}
	d.keyMap.Submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	d.keyMap.Close = key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", "cancel"))
	return d
}

func (*PromptCommand) ID() string { return PromptCommandID }

func (d *PromptCommand) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			return controlplaneCommandOrClose(d.data.CancelCommand)
		case key.Matches(keyMsg, d.keyMap.Submit):
			return ActionRunControlplaneCommand{Command: d.data.SubmitCommandPrefix + strings.TrimSpace(d.input.Value())}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *PromptCommand) Cursor() *uv.Cursor {
	cur := TextInputCursor(d.input, d.com.Styles.TextInput)
	if cur == nil {
		return nil
	}
	cur.X += 2
	cur.Y += 4
	return cur
}

func (d *PromptCommand) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	frame := commandui.NewFrame(area.Dx(), area.Dy())
	frame = frame.WithInnerWidth(0)
	d.input.SetWidth(max(1, frame.InnerWidth()-2))
	view := commandui.RenderPromptCard(frame, commandui.PromptData{
		Title: strings.TrimSpace(d.data.Title),
		Value: d.input.View(),
	})
	cur := d.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}
