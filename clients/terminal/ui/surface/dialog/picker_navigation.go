package dialog

import "strings"

func (p *Picker) selectedOption() *pickerOption {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return nil
	}
	if !p.visible[p.cursor].item.Selectable() {
		return nil
	}
	return &p.visible[p.cursor]
}

func (p *Picker) selectFirstSelectable() {
	for i, option := range p.visible {
		if option.item.Selectable() {
			p.cursor = i
			return
		}
	}
	p.cursor = -1
}

func (p *Picker) moveSelection(delta int) {
	if len(p.visible) == 0 {
		p.cursor = -1
		return
	}
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		p.selectFirstSelectable()
		return
	}
	for range p.visible {
		p.cursor += delta
		if p.cursor < 0 {
			p.cursor = len(p.visible) - 1
		}
		if p.cursor >= len(p.visible) {
			p.cursor = 0
		}
		if p.visible[p.cursor].item.Selectable() {
			return
		}
	}
	p.cursor = -1
}

func (p *Picker) shortcutAction(key string) Action {
	for _, option := range p.visible {
		if !option.item.Selectable() || strings.TrimSpace(option.item.Shortcut) == "" {
			continue
		}
		if key == option.item.Shortcut {
			return option.action
		}
	}
	return nil
}
