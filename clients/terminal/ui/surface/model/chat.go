package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/list"
)

// Chat represents the chat UI model that handles chat interactions and
// messages.
type Chat struct {
	com      *common.Common
	list     *list.List
	idInxMap map[string]int

	pausedAnimations map[string]struct{}

	mouseDown     bool
	mouseDownItem int
	mouseDownX    int
	mouseDownY    int
	mouseDragItem int
	mouseDragX    int
	mouseDragY    int

	lastClickTime time.Time
	lastClickX    int
	lastClickY    int
	clickCount    int

	pendingClickID int

	follow bool
}

// NewChat creates a new instance of [Chat] that handles chat interactions and
// messages.
func NewChat(com *common.Common) *Chat {
	c := &Chat{
		com:              com,
		idInxMap:         make(map[string]int),
		pausedAnimations: make(map[string]struct{}),
	}
	l := list.NewList()
	l.SetGap(1)
	l.RegisterRenderCallback(c.applyHighlightRange)
	l.RegisterRenderCallback(list.FocusedRenderCallback(l))
	c.list = l
	c.mouseDownItem = -1
	c.mouseDragItem = -1
	return c
}

// Height returns the height of the chat view port.
func (m *Chat) Height() int {
	return m.list.Height()
}

// View renders the chat viewport as styled text.
func (m *Chat) View() string {
	return m.list.Render()
}

// Draw renders the chat UI component to the screen and the given area.
func (m *Chat) Draw(scr uv.Screen, area uv.Rectangle) {
	uv.NewStyledString(m.View()).Draw(scr, area)
}

// SetSize sets the size of the chat view port.
func (m *Chat) SetSize(width, height int) {
	wasFollowing := m.follow
	viewport := m.SnapshotViewport()
	m.list.SetSize(width, height)
	if wasFollowing {
		m.ScrollToBottom()
		return
	}
	m.RestoreViewport(viewport)
}

// Len returns the number of items in the chat list.
func (m *Chat) Len() int {
	return m.list.Len()
}

// SetMessages sets the chat messages to the provided list of message items.
func (m *Chat) SetMessages(msgs ...chat.MessageItem) {
	wasEmpty := m.list.Len() == 0
	wasFollowing := m.follow
	viewport := m.SnapshotViewport()

	m.idInxMap = make(map[string]int)
	m.pausedAnimations = make(map[string]struct{})

	items := make([]list.Item, len(msgs))
	for i, msg := range msgs {
		m.indexMessage(msg, i)
		items[i] = msg
	}
	m.list.SetItems(items...)
	if wasEmpty || wasFollowing {
		m.ScrollToBottom()
		return
	}
	m.RestoreViewport(viewport)
}

func (m *Chat) indexMessage(msg chat.MessageItem, index int) {
	m.idInxMap[msg.ID()] = index
}

// Animate animates visible items in the chat list and tracks paused animations.
func (m *Chat) Animate(msg anim.StepMsg) tea.Cmd {
	idx, ok := m.idInxMap[msg.ID]
	if !ok {
		return nil
	}

	animatable, ok := m.list.ItemAt(idx).(chat.Animatable)
	if !ok {
		return nil
	}

	startIdx, endIdx := m.list.VisibleItemIndices()
	isVisible := idx >= startIdx && idx <= endIdx
	if !isVisible {
		m.pausedAnimations[msg.ID] = struct{}{}
		return nil
	}

	delete(m.pausedAnimations, msg.ID)
	return animatable.Animate(msg)
}

// RestartPausedVisibleAnimations restarts animations that became visible again.
func (m *Chat) RestartPausedVisibleAnimations() tea.Cmd {
	if len(m.pausedAnimations) == 0 {
		return nil
	}

	startIdx, endIdx := m.list.VisibleItemIndices()
	var cmds []tea.Cmd

	for id := range m.pausedAnimations {
		idx, ok := m.idInxMap[id]
		if !ok {
			delete(m.pausedAnimations, id)
			continue
		}

		if idx >= startIdx && idx <= endIdx {
			if animatable, ok := m.list.ItemAt(idx).(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			delete(m.pausedAnimations, id)
		}
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
