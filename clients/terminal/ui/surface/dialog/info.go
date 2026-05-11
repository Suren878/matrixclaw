package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const (
	InfoID              = "info"
	ServerStatusInfoID  = "server_status_info"
	ServerRestartInfoID = "server_restart_info"
)

type InfoData struct {
	ID          string
	Title       string
	Text        string
	Rows        []InfoRow
	CloseAction Action
}

type InfoRow = controlplane.InfoRow

type Info struct {
	data InfoData

	keyMap struct {
		Close key.Binding
	}
}

func NewInfo(_ *surfacecommon.Common, data InfoData) *Info {
	return &Info{
		data: data,
		keyMap: struct {
			Close key.Binding
		}{
			Close: key.NewBinding(key.WithKeys("esc", "enter", "alt+esc"), key.WithHelp("esc/enter", "close")),
		},
	}
}

func (d *Info) ID() string {
	if id := strings.TrimSpace(d.data.ID); id != "" {
		return id
	}
	return InfoID
}

func (d *Info) SetText(text string) {
	d.data.Text = text
	d.data.Rows = nil
}

func (d *Info) SetRows(rows []InfoRow) {
	d.data.Rows = append([]InfoRow(nil), rows...)
	d.data.Text = ""
}

func (d *Info) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if ok && key.Matches(keyMsg, d.keyMap.Close) {
		if d.data.CloseAction != nil {
			return d.data.CloseAction
		}
		return ActionClose{}
	}
	return nil
}

func (d *Info) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	frame := commandui.NewFrame(area.Dx(), area.Dy()).WithInnerWidth(0)
	items := d.infoItems()
	if len(items) == 0 {
		items = []commandui.Item{{Title: "(empty)", Disabled: true}}
	}
	view := commandui.RenderInfoCard(frame, commandui.InfoData{
		Title:          d.title(),
		Items:          items,
		Selected:       -1,
		Footer:         d.footerItems(),
		FooterSelected: 0,
		Help:           "esc/enter back",
	})
	DrawCenter(scr, area, view)
	return nil
}

func (d *Info) title() string {
	if title := strings.TrimSpace(d.data.Title); title != "" {
		return title
	}
	return "Info"
}

func (d *Info) footerItems() []commandui.Item {
	return []commandui.Item{{ID: "back", Title: "Back", Role: commandui.RoleBack}}
}

func (d *Info) infoItems() []commandui.Item {
	if len(d.data.Rows) > 0 {
		return infoRowItems(d.data.Rows)
	}
	return infoTextItems(d.data.Text)
}

func infoRowItems(rows []InfoRow) []commandui.Item {
	items := make([]commandui.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, commandui.Item{
			Title:  strings.TrimSpace(row.Label),
			Status: strings.TrimSpace(row.Value),
		})
	}
	return items
}

func infoTextItems(text string) []commandui.Item {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	items := make([]commandui.Item, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			items = append(items, commandui.Item{})
			continue
		}
		if label, value, ok := strings.Cut(line, "\t"); ok {
			items = append(items, commandui.Item{Title: strings.TrimSpace(label), Status: strings.TrimSpace(value)})
			continue
		}
		if label, value, ok := strings.Cut(line, ": "); ok {
			items = append(items, commandui.Item{Title: strings.TrimSpace(label), Status: strings.TrimSpace(value)})
			continue
		}
		items = append(items, commandui.Item{Title: line, Disabled: true})
	}
	return items
}
