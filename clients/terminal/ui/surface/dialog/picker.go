package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	terminaltextfield "github.com/Suren878/matrixclaw/clients/terminal/ui/textfield"
)

// PickerID is the identifier for the generic picker popup.
const PickerID = "picker"

type ListEntryKind int

const (
	ListEntryRow ListEntryKind = iota
	ListEntryHeader
	ListEntryDivider
)

type PickerEntry struct {
	Kind     ListEntryKind
	ID       string
	Title    string
	Status   string
	Shortcut string
	Role     commandui.Role
	Tone     commandui.RowTone
	Selected bool
	Footer   bool
	Action   Action
}

type PickerData struct {
	ID          string
	Title       string
	Legend      string
	Filter      bool
	Entries     []PickerEntry
	CloseAction Action
}

type pickerOption struct {
	item     commandui.Item
	action   Action
	selected bool
	footer   bool
}

// Picker is a generic list picker with optional search and grouped entries.
type Picker struct {
	id      string
	input   terminaltextfield.Model
	options []pickerOption
	visible []pickerOption
	cursor  int
	data    PickerData

	keyMap struct {
		Select   key.Binding
		UpDown   key.Binding
		Next     key.Binding
		Previous key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Picker)(nil)

func NewPicker(com *surfacecommon.Common, data PickerData) *Picker {
	if com == nil {
		com = surfacecommon.DefaultCommon()
	}

	p := &Picker{
		id: strings.TrimSpace(data.ID),
		data: PickerData{
			ID:          strings.TrimSpace(data.ID),
			Title:       data.Title,
			Legend:      data.Legend,
			Filter:      data.Filter,
			Entries:     append([]PickerEntry(nil), data.Entries...),
			CloseAction: data.CloseAction,
		},
	}
	if p.id == "" {
		p.id = PickerID
	}

	p.input = terminaltextfield.New("Search", "", terminaltextfield.WithSurfaceStyles(com.Styles.TextInput))

	p.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	p.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	p.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓", "next item"),
	)
	p.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑", "previous item"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	p.keyMap.Close = closeKey

	p.setItems()

	return p
}

func (p *Picker) ID() string {
	return p.id
}

func (p *Picker) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Close):
			if p.data.CloseAction != nil {
				return p.data.CloseAction
			}
			return ActionClose{}
		case key.Matches(msg, p.keyMap.Previous):
			p.moveSelection(-1)
		case key.Matches(msg, p.keyMap.Next):
			p.moveSelection(1)
		case key.Matches(msg, p.keyMap.Select):
			if option := p.selectedOption(); option != nil {
				return option.action
			}
		default:
			if action := p.shortcutAction(msg.String()); action != nil {
				return action
			}
			if !p.data.Filter {
				return nil
			}
			cmd := p.input.Update(msg)
			p.applyFilter()
			if cmd != nil {
				return ActionCmd{Cmd: cmd}
			}
		}
	}

	return nil
}
