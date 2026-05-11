package setup

import (
	"strings"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

func (m *model) renderEditableForm(title string, items []listItem, extras ...string) string {
	extraLines := make([]string, 0, len(extras))
	for _, extra := range extras {
		if strings.TrimSpace(extra) != "" {
			extraLines = append(extraLines, extra)
		}
	}
	card := commandui.RenderFormCard(m.commandFrame(), commandui.FormData{
		Title:      title,
		Fields:     commandItems(items),
		Focus:      formFocus(m.formFocus, len(items)),
		Buttons:    setupFormButtons(),
		Button:     m.formAction,
		Error:      m.formError,
		ExtraLines: extraLines,
	})
	return m.renderCommandCard(card)
}

func (m *model) renderStepList(title string, meta string, itemTitle string, itemStatus string) string {
	card := commandui.RenderListCard(m.commandFrame(), commandui.ListData{
		Title:    title,
		Meta:     meta,
		Items:    commandItems([]listItem{{Title: "Continue"}, {Title: itemTitle, Status: itemStatus}}),
		Selected: m.cursor,
		Help:     "enter select · ↑/↓ move · esc back",
	})
	return m.renderCommandCard(card)
}

func (m *model) renderPickerFrame(title string, items []listItem, cursor int) string {
	card := commandui.RenderPickerCard(m.commandFrame(), commandui.PickerData{
		Title:    title,
		Items:    commandItems(items),
		Selected: cursor,
	})
	return m.renderCommandCard(card)
}

func commandItems(items []listItem) []commandui.Item {
	out := make([]commandui.Item, 0, len(items))
	for _, item := range items {
		out = append(out, commandui.Item{
			Title:    item.Title,
			Status:   item.Status,
			Disabled: item.Disabled,
			Tone:     item.Tone,
		})
	}
	return out
}

func formFocus(focus int, fieldCount int) commandui.FormFocus {
	if focus >= fieldCount {
		return commandui.FormFocus{Kind: commandui.FormFocusButton}
	}
	return commandui.FormFocus{Kind: commandui.FormFocusField, Index: focus}
}

func setupFormButtons() []commandui.ButtonSpec {
	return []commandui.ButtonSpec{
		{Label: "Save", Role: commandui.RoleSubmit},
		{Label: "Cancel", Role: commandui.RoleCancel},
	}
}
