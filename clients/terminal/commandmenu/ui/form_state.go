package commandui

type FormFocusKind int

const (
	FormFocusField FormFocusKind = iota
	FormFocusButton
)

type FormFocus struct {
	Kind  FormFocusKind
	Index int
}

type FormState struct {
	Focus  FormFocus
	Button int
}

func (s *FormState) Update(key string, fields []Item, buttons []ButtonSpec, closeRole Role) Event {
	switch key {
	case "esc", "q":
		return eventForRole(closeRole)
	case "up", "k":
		s.move(-1, len(fields), len(buttons))
	case "down", "j":
		s.move(1, len(fields), len(buttons))
	case "left", "h":
		if s.Focus.Kind == FormFocusButton {
			s.Button = max(0, s.Button-1)
		}
	case "right", "l":
		if s.Focus.Kind == FormFocusButton && len(buttons) > 0 {
			s.Button = min(len(buttons)-1, s.Button+1)
		}
	case "enter":
		if s.Focus.Kind == FormFocusButton {
			if s.Button >= 0 && s.Button < len(buttons) {
				return eventForRole(buttons[s.Button].Role)
			}
			return Event{}
		}
		if s.Focus.Index >= 0 && s.Focus.Index < len(fields) {
			return Event{Kind: EventEdit, ID: fields[s.Focus.Index].ID}
		}
	}
	return Event{}
}

func (s *FormState) move(delta int, fieldCount int, buttonCount int) {
	maxIndex := fieldCount
	if buttonCount == 0 {
		maxIndex = fieldCount - 1
	}
	current := s.Focus.Index
	if s.Focus.Kind == FormFocusButton {
		current = fieldCount
	}
	current += delta
	if current < 0 {
		current = maxIndex
	}
	if current > maxIndex {
		current = 0
	}
	if buttonCount > 0 && current == fieldCount {
		s.Focus = FormFocus{Kind: FormFocusButton}
		if s.Button < 0 || s.Button >= buttonCount {
			s.Button = 0
		}
		return
	}
	s.Focus = FormFocus{Kind: FormFocusField, Index: current}
}
