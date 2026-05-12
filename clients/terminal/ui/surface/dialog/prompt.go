package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	terminaltextfield "github.com/Suren878/matrixclaw/clients/terminal/ui/textfield"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const PromptCommandID = "prompt_command"

type PromptCommandData = controlplane.PromptData

type PromptCommand struct {
	com   *surfacecommon.Common
	input terminaltextfield.Model
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
	d := &PromptCommand{
		com: com,
		input: terminaltextfield.New(
			strings.TrimSpace(data.Placeholder),
			strings.TrimSpace(data.Value),
			terminaltextfield.WithSecret(data.Sensitive),
			terminaltextfield.WithSurfaceStyles(com.Styles.TextInput),
		),
		data: data,
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
	cmd = d.input.Update(msg)
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
