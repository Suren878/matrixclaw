package runtime

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

func (m *appModel) ShortHelp() []key.Binding {
	km := m.input.KeyMap()
	tab := km.Tab
	sessions := km.Sessions
	quit := km.Quit
	helpKey := km.Help

	if m.input.focus == appFocusEditor {
		tab.SetHelp("tab", "focus chat")
		out := []key.Binding{
			km.Commands,
			tab,
			sessions,
			km.Editor.Newline,
			quit,
			helpKey,
		}
		if value := strings.TrimSpace(m.input.Value()); value == "" {
			out = append([]key.Binding{km.Chat.Todo}, out...)
		}
		return out
	}

	tab.SetHelp("tab", "focus editor")
	return []key.Binding{
		km.Commands,
		tab,
		sessions,
		km.Chat.UpDown,
		km.Chat.View,
		km.Chat.Copy,
		quit,
		helpKey,
	}
}

func (m *appModel) FullHelp() [][]key.Binding {
	km := m.input.KeyMap()
	helpKey := km.Help
	if m.help.ShowAll {
		helpKey.SetHelp("ctrl+g", "less")
	}

	if m.input.focus == appFocusEditor {
		return [][]key.Binding{
			{
				km.Commands,
				km.Tab,
				km.Sessions,
				km.Chat.Todo,
				helpKey,
			},
			{
				km.Editor.SendMessage,
				km.Editor.Newline,
				km.Editor.OpenEditor,
				km.Quit,
			},
		}
	}

	return [][]key.Binding{
		{
			km.Commands,
			km.Tab,
			km.Sessions,
			helpKey,
		},
		{
			km.Chat.UpDown,
			km.Chat.HalfPageDown,
			km.Chat.HalfPageUp,
			km.Chat.View,
			km.Chat.Copy,
		},
		{
			km.Chat.PageDown,
			km.Chat.PageUp,
			km.Chat.Home,
			km.Chat.End,
		},
		{
			km.Chat.Expand,
			km.Quit,
		},
	}
}
