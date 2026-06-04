package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	terminaltextfield "github.com/Suren878/matrixclaw/clients/terminal/ui/textfield"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const PromptCommandID = "prompt_command"

type PromptCommandData = controlplane.PromptData

type PromptCommand struct {
	com     *surfacecommon.Common
	input   terminaltextfield.Model
	data    PromptCommandData
	loading bool
	frame   int

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
	if _, ok := msg.(loadingTickMsg); ok {
		if !d.loading {
			return nil
		}
		d.frame = (d.frame + 1) % len(loadingFrames)
		return ActionCmd{Cmd: loadingTickCmd()}
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			if command := strings.TrimSpace(d.data.CancelCommand); command != "" {
				return ActionRunControlplaneCommand{Command: command, CloseSource: true}
			}
			return ActionClose{}
		case key.Matches(keyMsg, d.keyMap.Submit):
			return ActionRunControlplaneCommand{Command: d.data.SubmitCommandPrefix + strings.TrimSpace(d.input.Value()), CloseSource: true}
		}
	}
	cmd := d.input.Update(msg)
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
	frame := components.NewFrame(area.Dx(), area.Dy())
	frame = frame.WithInnerWidth(0)
	d.input.SetWidth(max(1, frame.InnerWidth()-2))
	view := components.RenderPromptCard(frame, components.PromptData{
		Title: loadingTitle(d.data.Title, d.loading, d.frame),
		Value: d.input.View(),
	})
	cur := d.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (d *PromptCommand) StartLoading() tea.Cmd {
	d.loading = true
	d.frame = 0
	return loadingTickCmd()
}

func (d *PromptCommand) StopLoading() {
	d.loading = false
	d.frame = 0
}
