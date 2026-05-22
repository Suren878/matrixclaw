package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const FormCommandID = "form_command"

type FormCommandData = controlplane.FormData
type FormCommandField = controlplane.FormField

type FormCommand struct {
	data    FormCommandData
	state   commandui.FormState
	loading bool
	frame   int
}

func NewFormCommand(_ *surfacecommon.Common, data FormCommandData) *FormCommand {
	return &FormCommand{
		data: data,
		state: commandui.FormState{
			Focus: commandui.FormFocus{Kind: commandui.FormFocusField},
		},
	}
}

func (*FormCommand) ID() string { return FormCommandID }

func (d *FormCommand) HandleMsg(msg tea.Msg) Action {
	if _, ok := msg.(loadingTickMsg); ok {
		if !d.loading {
			return nil
		}
		d.frame = (d.frame + 1) % len(loadingFrames)
		return ActionCmd{Cmd: loadingTickCmd()}
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	event := d.state.Update(keyMsg.String(), d.items(), d.buttons(), commandui.RoleBack)
	switch event.Kind {
	case commandui.EventEdit:
		for _, field := range d.data.Fields {
			if field.ID == event.ID && strings.TrimSpace(field.EditCommand) != "" {
				return ActionRunControlplaneCommand{Command: strings.TrimSpace(field.EditCommand)}
			}
		}
	case commandui.EventSubmit:
		if strings.TrimSpace(d.data.SubmitCommand) != "" {
			return ActionRunControlplaneCommand{Command: strings.TrimSpace(d.data.SubmitCommand), CloseSource: true}
		}
	case commandui.EventBack, commandui.EventCancel:
		return d.cancelAction()
	}
	return nil
}

func (d *FormCommand) cancelAction() Action {
	if command := strings.TrimSpace(d.data.CancelCommand); command != "" {
		return ActionRunControlplaneCommand{Command: command, CloseSource: true}
	}
	return ActionClose{}
}

func (d *FormCommand) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	view := commandui.RenderFormCard(commandui.NewFrame(area.Dx(), area.Dy()), commandui.FormData{
		Title:   loadingTitle(d.title(), d.loading, d.frame),
		Fields:  d.items(),
		Focus:   d.state.Focus,
		Buttons: d.buttons(),
		Button:  d.state.Button,
		Error:   d.data.Error,
		Help:    "enter edit · ↑/↓ move · ←/→ action · esc cancel",
	})
	DrawCenter(scr, area, view)
	return nil
}

func (d *FormCommand) StartLoading() tea.Cmd {
	d.loading = true
	d.frame = 0
	return loadingTickCmd()
}

func (d *FormCommand) StopLoading() {
	d.loading = false
	d.frame = 0
}

func (d *FormCommand) title() string {
	if title := strings.TrimSpace(d.data.Title); title != "" {
		return title
	}
	return "Form"
}

func (d *FormCommand) items() []commandui.Item {
	items := make([]commandui.Item, 0, len(d.data.Fields))
	for _, field := range d.data.Fields {
		tone := commandui.RowToneNormal
		if strings.TrimSpace(field.ID) == "model" && strings.TrimSpace(field.Value) != "" && !field.Disabled {
			tone = commandui.RowToneAccent
		}
		items = append(items, commandui.Item{
			ID:       strings.TrimSpace(field.ID),
			Title:    strings.TrimSpace(field.Label),
			Status:   strings.TrimSpace(field.Value),
			Tone:     tone,
			Disabled: field.Disabled,
		})
	}
	return items
}

func (d *FormCommand) buttons() []commandui.ButtonSpec {
	return []commandui.ButtonSpec{
		{Label: firstNonEmptyTrimmed(d.data.SubmitLabel, "Save"), Role: commandui.RoleSubmit},
		{Label: firstNonEmptyTrimmed(d.data.CancelLabel, "Cancel"), Role: commandui.RoleBack},
	}
}
