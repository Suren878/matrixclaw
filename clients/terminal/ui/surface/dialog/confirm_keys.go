package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type twoButtonConfirmKeyMap struct {
	Choose key.Binding
	Select key.Binding
	Close  key.Binding
}

func defaultTwoButtonConfirmKeyMap() twoButtonConfirmKeyMap {
	return twoButtonConfirmKeyMap{
		Choose: key.NewBinding(key.WithKeys("left", "right", "tab"), key.WithHelp("←/→", "switch")),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Close:  key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", "cancel")),
	}
}

type twoButtonConfirmKeyAction int

const (
	twoButtonConfirmKeyNone twoButtonConfirmKeyAction = iota
	twoButtonConfirmKeyClose
	twoButtonConfirmKeySelect
)

func handleTwoButtonConfirmKey(msg tea.Msg, selected *int, keyMap twoButtonConfirmKeyMap) twoButtonConfirmKeyAction {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return twoButtonConfirmKeyNone
	}

	switch {
	case key.Matches(keyMsg, keyMap.Close):
		return twoButtonConfirmKeyClose
	case key.Matches(keyMsg, keyMap.Select):
		return twoButtonConfirmKeySelect
	case key.Matches(keyMsg, keyMap.Choose):
		*selected = (*selected + 1) % 2
	}
	return twoButtonConfirmKeyNone
}
