package commandui

type TextEditState struct {
	ButtonsFocused bool
	Button         int
}

func (s *TextEditState) Update(key string, buttons []ButtonSpec, closeRole Role) Event {
	switch key {
	case "esc":
		return eventForRole(closeRole)
	case "ctrl+s":
		return Event{Kind: EventSubmit, ID: "save"}
	case "tab":
		s.ButtonsFocused = !s.ButtonsFocused
		s.clampButton(len(buttons))
	case "up", "k":
		if s.ButtonsFocused {
			s.ButtonsFocused = false
		}
	case "left", "h":
		if s.ButtonsFocused {
			s.Button = max(0, s.Button-1)
		}
	case "right", "l":
		if s.ButtonsFocused {
			s.Button = min(max(0, len(buttons)-1), s.Button+1)
		}
	case "enter":
		if s.ButtonsFocused {
			s.clampButton(len(buttons))
			if s.Button >= 0 && s.Button < len(buttons) {
				return eventForRole(buttons[s.Button].Role)
			}
		}
	}
	return Event{}
}

func (s *TextEditState) clampButton(count int) {
	if count <= 0 {
		s.Button = 0
		return
	}
	if s.Button < 0 {
		s.Button = 0
	}
	if s.Button >= count {
		s.Button = count - 1
	}
}
