package setup

import (
	"strings"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
)

func (m *model) renderEditableForm(title string, items []listItem, extras ...string) string {
	extraLines := make([]string, 0, len(extras))
	for _, extra := range extras {
		if strings.TrimSpace(extra) != "" {
			extraLines = append(extraLines, extra)
		}
	}
	card := components.RenderFormCard(m.commandFrame(), components.FormData{
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
	card := components.RenderListCard(m.commandFrame(), components.ListData{
		Title:    title,
		Meta:     meta,
		Items:    commandItems([]listItem{{Title: "Continue"}, {Title: itemTitle, Status: itemStatus}}),
		Selected: m.cursor,
		Help:     "enter select · ↑/↓ move · esc back",
	})
	return m.renderCommandCard(card)
}

func (m *model) renderPickerFrame(title string, items []listItem, cursor int) string {
	card := components.RenderPickerCard(m.commandFrame(), components.PickerData{
		Title:    title,
		Items:    commandItems(items),
		Selected: cursor,
	})
	return m.renderCommandCard(card)
}

func commandItems(items []listItem) []components.Item {
	out := make([]components.Item, 0, len(items))
	for _, item := range items {
		out = append(out, components.Item{
			Title:    item.Title,
			Status:   item.Status,
			Disabled: item.Disabled,
			Tone:     item.Tone,
		})
	}
	return out
}

func formFocus(focus int, fieldCount int) components.FormFocus {
	if focus >= fieldCount {
		return components.FormFocus{Kind: components.FormFocusButton}
	}
	return components.FormFocus{Kind: components.FormFocusField, Index: focus}
}

func setupFormButtons() []components.ButtonSpec {
	return []components.ButtonSpec{
		{Label: "Save", Role: components.RoleSubmit},
		{Label: "Cancel", Role: components.RoleCancel},
	}
}
