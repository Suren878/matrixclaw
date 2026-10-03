package model

import "charm.land/bubbles/v2/key"

// KeyMap holds the terminal key bindings that the key handlers match and the
// help bar shows.
type KeyMap struct {
	Editor struct {
		SendMessage key.Binding
		OpenEditor  key.Binding
		Newline     key.Binding
		AttachFiles key.Binding
		Commands    key.Binding

		AttachmentDeleteMode key.Binding
		Escape               key.Binding
		DeleteAllAttachments key.Binding

		HistoryPrev key.Binding
		HistoryNext key.Binding
	}

	Chat struct {
		Todo         key.Binding
		Cancel       key.Binding
		Reload       key.Binding
		Up           key.Binding
		Down         key.Binding
		UpDown       key.Binding
		PageDown     key.Binding
		PageUp       key.Binding
		HalfPageDown key.Binding
		HalfPageUp   key.Binding
		Home         key.Binding
		End          key.Binding
		Copy         key.Binding
		Expand       key.Binding
		View         key.Binding
	}

	Quit     key.Binding
	Help     key.Binding
	Commands key.Binding
	Sessions key.Binding
	Tab      key.Binding
}

func DefaultKeyMap() KeyMap {
	km := KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "more"),
		),
		Commands: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
		Sessions: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "sessions"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "change focus"),
		),
	}

	km.Editor.SendMessage = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "send"),
	)
	km.Editor.OpenEditor = key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "open editor"),
	)
	km.Editor.Newline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("ctrl+j", "newline"),
	)
	km.Editor.AttachFiles = key.NewBinding(
		key.WithKeys("ctrl+f", "ctrl+v"),
		key.WithHelp("ctrl+f", "attach the file paths typed in the editor"),
	)
	km.Editor.Commands = key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "commands"),
	)
	km.Editor.AttachmentDeleteMode = key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r+{i}", "delete attachment at index i"),
	)
	km.Editor.Escape = key.NewBinding(
		key.WithKeys("esc", "alt+esc"),
		key.WithHelp("esc", "exit delete mode"),
	)
	km.Editor.DeleteAllAttachments = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("ctrl+r+r", "delete all attachments"),
	)
	km.Editor.HistoryPrev = key.NewBinding(
		key.WithKeys("up"),
	)
	km.Editor.HistoryNext = key.NewBinding(
		key.WithKeys("down"),
	)

	km.Chat.Todo = key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "todo"),
	)
	km.Chat.Cancel = key.NewBinding(
		key.WithKeys("esc", "alt+esc"),
		key.WithHelp("esc", "cancel run"),
	)
	km.Chat.Reload = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "reload"),
	)
	km.Chat.Up = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑", "up"),
	)
	km.Chat.Down = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓", "down"),
	)
	km.Chat.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "scroll"),
	)
	km.Chat.HalfPageDown = key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "half page down"),
	)
	km.Chat.PageDown = key.NewBinding(
		key.WithKeys("pgdown", "f"),
		key.WithHelp("f/pgdn", "page down"),
	)
	km.Chat.PageUp = key.NewBinding(
		key.WithKeys("pgup", "b"),
		key.WithHelp("b/pgup", "page up"),
	)
	km.Chat.HalfPageUp = key.NewBinding(
		key.WithKeys("u", "ctrl+u"),
		key.WithHelp("u", "half page up"),
	)
	km.Chat.Home = key.NewBinding(
		key.WithKeys("g", "home"),
		key.WithHelp("g", "home"),
	)
	km.Chat.End = key.NewBinding(
		key.WithKeys("G", "end"),
		key.WithHelp("G", "end"),
	)
	km.Chat.Copy = key.NewBinding(
		key.WithKeys("c", "y", "C", "Y"),
		key.WithHelp("c/y", "copy"),
	)
	km.Chat.Expand = key.NewBinding(
		key.WithKeys("space"),
		key.WithHelp("space", "expand/collapse"),
	)
	km.Chat.View = key.NewBinding(
		key.WithKeys("enter", "v"),
		key.WithHelp("enter/v", "view"),
	)

	return km
}
