package runtime

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

func (m *appModel) ShortHelp() []key.Binding {
	km := m.input.KeyMap()
	tab := copyBinding(km.Tab)
	sessions := copyBinding(km.Sessions)
	quit := copyBinding(km.Quit)
	helpKey := copyBinding(km.Help)

	if m.focus == appFocusEditor {
		tab.SetHelp("tab", "focus chat")
		out := []key.Binding{
			copyBinding(km.Commands),
			tab,
			sessions,
			copyBinding(km.Editor.Newline),
			quit,
			helpKey,
		}
		if value := strings.TrimSpace(m.input.Value()); value == "" {
			out = append([]key.Binding{copyBinding(km.Chat.NewSession)}, out...)
		}
		return out
	}

	tab.SetHelp("tab", "focus editor")
	return []key.Binding{
		copyBinding(km.Commands),
		tab,
		sessions,
		copyBinding(km.Chat.UpDown),
		copyBinding(km.Chat.Copy),
		quit,
		helpKey,
	}
}

func (m *appModel) FullHelp() [][]key.Binding {
	km := m.input.KeyMap()
	helpKey := copyBinding(km.Help)
	if m.help.ShowAll {
		helpKey.SetHelp("ctrl+g", "less")
	}

	if m.focus == appFocusEditor {
		return [][]key.Binding{
			{
				copyBinding(km.Commands),
				copyBinding(km.Tab),
				copyBinding(km.Sessions),
				copyBinding(km.Chat.NewSession),
				helpKey,
			},
			{
				copyBinding(km.Editor.SendMessage),
				copyBinding(km.Editor.Newline),
				copyBinding(km.Editor.OpenEditor),
				copyBinding(km.Quit),
			},
		}
	}

	return [][]key.Binding{
		{
			copyBinding(km.Commands),
			copyBinding(km.Tab),
			copyBinding(km.Sessions),
			helpKey,
		},
		{
			copyBinding(km.Chat.UpDown),
			copyBinding(km.Chat.HalfPageDown),
			copyBinding(km.Chat.HalfPageUp),
			copyBinding(km.Chat.Copy),
		},
		{
			copyBinding(km.Chat.PageDown),
			copyBinding(km.Chat.PageUp),
			copyBinding(km.Chat.Home),
			copyBinding(km.Chat.End),
		},
		{
			copyBinding(km.Chat.Expand),
			copyBinding(km.Quit),
		},
	}
}

func copyBinding(binding key.Binding) key.Binding {
	return binding
}
