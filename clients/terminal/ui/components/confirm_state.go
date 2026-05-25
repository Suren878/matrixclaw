package components

type ConfirmState struct {
	Selected int
}

func (s *ConfirmState) Update(key string) Event {
	switch key {
	case "esc", "alt+esc", "n":
		return Event{Kind: EventCancel, ID: "cancel"}
	case "enter", "y":
		if s.Selected == 0 || key == "y" {
			return Event{Kind: EventSubmit, ID: "confirm"}
		}
		return Event{Kind: EventCancel, ID: "cancel"}
	case "left", "h", "up", "k":
		if s.Selected > 0 {
			s.Selected--
		}
	case "right", "l", "down", "j", "tab":
		if s.Selected < 1 {
			s.Selected++
		}
	}
	return Event{}
}
