package model

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/list"
)

// Focus sets the focus state of the chat component.
func (m *Chat) Focus() {
	m.list.Focus()
}

// Blur removes the focus state from the chat component.
func (m *Chat) Blur() {
	m.list.Blur()
}

// AtBottom returns whether the chat list is currently scrolled to the bottom.
func (m *Chat) AtBottom() bool {
	return m.list.AtBottom()
}

// Follow returns whether the chat view is in follow mode.
func (m *Chat) Follow() bool {
	return m.follow
}

// ScrollToBottom scrolls the chat view to the bottom.
func (m *Chat) ScrollToBottom() {
	m.list.ScrollToBottom()
	m.follow = true
}

// ScrollToTop scrolls the chat view to the top.
func (m *Chat) ScrollToTop() {
	m.list.ScrollToTop()
	m.follow = false
}

// ScrollBy scrolls the chat view by the given number of line deltas.
func (m *Chat) ScrollBy(lines int) {
	m.list.ScrollBy(lines)
	m.follow = lines > 0 && m.AtBottom()
}

// ScrollToSelected scrolls the chat view to the selected item.
func (m *Chat) ScrollToSelected() {
	m.list.ScrollToSelected()
	m.follow = m.AtBottom()
}

// ScrollToIndex scrolls the chat view to the item at the given index.
func (m *Chat) ScrollToIndex(index int) {
	m.list.ScrollToIndex(index)
	m.follow = m.AtBottom()
}

func (m *Chat) ScrollToTopAndAnimate() tea.Cmd {
	m.ScrollToTop()
	return m.RestartPausedVisibleAnimations()
}

func (m *Chat) ScrollToBottomAndAnimate() tea.Cmd {
	m.ScrollToBottom()
	return m.RestartPausedVisibleAnimations()
}

func (m *Chat) ScrollByAndAnimate(lines int) tea.Cmd {
	m.ScrollBy(lines)
	return m.RestartPausedVisibleAnimations()
}

func (m *Chat) ScrollToSelectedAndAnimate() tea.Cmd {
	m.ScrollToSelected()
	return m.RestartPausedVisibleAnimations()
}

// SelectedIndex returns the currently selected item index.
func (m *Chat) SelectedIndex() int {
	return m.list.Selected()
}

// SelectedMessageID returns the selected message ID, if any.
func (m *Chat) SelectedMessageID() string {
	item, ok := m.list.SelectedItem().(chat.MessageItem)
	if !ok {
		return ""
	}
	return item.ID()
}

// SetSelectedByID selects a message by its ID.
func (m *Chat) SetSelectedByID(id string) bool {
	idx, ok := m.idInxMap[id]
	if !ok {
		return false
	}
	m.SetSelected(idx)
	return true
}

// SelectedItemInView returns whether the selected item is currently in view.
func (m *Chat) SelectedItemInView() bool {
	return m.list.SelectedItemInView()
}

func (m *Chat) isSelectable(index int) bool {
	item := m.list.ItemAt(index)
	if item == nil {
		return false
	}
	_, ok := item.(list.Focusable)
	return ok
}

// SetSelected sets the selected message index in the chat list.
func (m *Chat) SetSelected(index int) {
	m.list.SetSelected(index)
	if index < 0 || index >= m.list.Len() {
		return
	}
	for {
		if m.isSelectable(m.list.Selected()) {
			return
		}
		if m.list.SelectNext() {
			continue
		}
		for {
			if !m.list.SelectPrev() {
				return
			}
			if m.isSelectable(m.list.Selected()) {
				return
			}
		}
	}
}

// SelectPrev selects the previous message in the chat list.
func (m *Chat) SelectPrev() {
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectNext selects the next message in the chat list.
func (m *Chat) SelectNext() {
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirst selects the first message in the chat list.
func (m *Chat) SelectFirst() {
	if !m.list.SelectFirst() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectLast selects the last message in the chat list.
func (m *Chat) SelectLast() {
	if !m.list.SelectLast() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirstInView selects the first message currently in view.
func (m *Chat) SelectFirstInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := startIdx; i <= endIdx; i++ {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// SelectLastInView selects the last message currently in view.
func (m *Chat) SelectLastInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := endIdx; i >= startIdx; i-- {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// ClearMessages removes all messages from the chat list.
func (m *Chat) ClearMessages() {
	m.idInxMap = make(map[string]int)
	m.pausedAnimations = make(map[string]struct{})
	m.list.SetItems()
	m.ClearMouse()
}

// RemoveMessage removes a message from the chat list by its ID.
func (m *Chat) RemoveMessage(id string) {
	idx, ok := m.idInxMap[id]
	if !ok {
		return
	}

	m.list.RemoveItem(idx)
	delete(m.idInxMap, id)

	for i := idx; i < m.list.Len(); i++ {
		if item, ok := m.list.ItemAt(i).(chat.MessageItem); ok {
			m.idInxMap[item.ID()] = i
		}
	}

	delete(m.pausedAnimations, id)
}

// MessageItem returns the message item with the given ID, or nil if not found.
func (m *Chat) MessageItem(id string) chat.MessageItem {
	idx, ok := m.idInxMap[id]
	if !ok {
		return nil
	}
	item, ok := m.list.ItemAt(idx).(chat.MessageItem)
	if !ok {
		return nil
	}
	return item
}

// ToggleExpandedSelectedItem expands the selected message item if it is expandable.
func (m *Chat) ToggleExpandedSelectedItem() {
	if expandable, ok := m.list.SelectedItem().(chat.Expandable); ok {
		if !expandable.ToggleExpanded() {
			m.ScrollToIndex(m.list.Selected())
		}
		if m.AtBottom() {
			m.ScrollToBottom()
		}
	}
}

// HandleKeyMsg handles key events for the chat component.
func (m *Chat) HandleKeyMsg(key tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.list.Focused() {
		if handler, ok := m.list.SelectedItem().(chat.KeyEventHandler); ok {
			return handler.HandleKeyEvent(key)
		}
	}
	return false, nil
}
