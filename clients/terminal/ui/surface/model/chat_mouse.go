package model

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/words"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/list"
)

// Constants for multi-click detection.
const (
	doubleClickThreshold = 400 * time.Millisecond
	clickTolerance       = 2
	mouseScrollLines     = 5
)

// DelayedClickMsg is sent after the double-click threshold to trigger a
// single-click action if no double-click occurred.
type DelayedClickMsg struct {
	ClickID int
	ItemIdx int
	X, Y    int
}

// HandleViewportMouse handles mouse input in chat-local coordinates.
func (m *Chat) HandleViewportMouse(msg tea.MouseMsg, x, y int) (bool, tea.Cmd) {
	switch mouse := msg.Mouse(); {
	case isWheelMouse(msg):
		if y < 0 || y >= m.Height() {
			return false, nil
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			cmd := m.ScrollByAndAnimate(-mouseScrollLines)
			if !m.SelectedItemInView() {
				m.SelectPrev()
				if scmd := m.ScrollToSelectedAndAnimate(); scmd != nil {
					cmd = tea.Batch(cmd, scmd)
				}
			}
			return true, cmd
		case tea.MouseWheelDown:
			cmd := m.ScrollByAndAnimate(mouseScrollLines)
			if !m.SelectedItemInView() {
				if m.AtBottom() {
					m.SelectLast()
				} else {
					m.SelectNext()
				}
				if scmd := m.ScrollToSelectedAndAnimate(); scmd != nil {
					cmd = tea.Batch(cmd, scmd)
				}
			}
			return true, cmd
		default:
			return true, nil
		}
	case isMouseClick(msg):
		if y < 0 || y >= m.Height() {
			return false, nil
		}
		return m.HandleMouseDown(x, y)
	case isMouseMotion(msg):
		if y <= 0 {
			cmd := m.ScrollByAndAnimate(-1)
			if !m.SelectedItemInView() {
				m.SelectPrev()
				if scmd := m.ScrollToSelectedAndAnimate(); scmd != nil {
					cmd = tea.Batch(cmd, scmd)
				}
			}
			m.HandleMouseDrag(x, y)
			return true, cmd
		}
		if y >= m.Height()-1 {
			cmd := m.ScrollByAndAnimate(1)
			if !m.SelectedItemInView() {
				m.SelectNext()
				if scmd := m.ScrollToSelectedAndAnimate(); scmd != nil {
					cmd = tea.Batch(cmd, scmd)
				}
			}
			m.HandleMouseDrag(x, y)
			return true, cmd
		}
		return m.HandleMouseDrag(x, y), nil
	case isMouseRelease(msg):
		return m.HandleMouseUp(x, y), nil
	default:
		return false, nil
	}
}

// HandleMouseDown handles mouse down events for the chat component.
func (m *Chat) HandleMouseDown(x, y int) (bool, tea.Cmd) {
	if m.list.Len() == 0 {
		return false, nil
	}

	itemIdx, itemY := m.list.ItemIndexAtPosition(x, y)
	if itemIdx < 0 {
		return false, nil
	}
	if !m.isSelectable(itemIdx) {
		return false, nil
	}

	m.pendingClickID++
	clickID := m.pendingClickID

	now := time.Now()
	if now.Sub(m.lastClickTime) <= doubleClickThreshold &&
		abs(x-m.lastClickX) <= clickTolerance &&
		abs(y-m.lastClickY) <= clickTolerance {
		m.clickCount++
	} else {
		m.clickCount = 1
	}
	m.lastClickTime = now
	m.lastClickX = x
	m.lastClickY = y

	m.list.SetSelected(itemIdx)

	var cmd tea.Cmd
	switch m.clickCount {
	case 1:
		m.mouseDown = true
		m.mouseDownItem = itemIdx
		m.mouseDownX = x
		m.mouseDownY = itemY
		m.mouseDragItem = itemIdx
		m.mouseDragX = x
		m.mouseDragY = itemY

		cmd = tea.Tick(doubleClickThreshold, func(t time.Time) tea.Msg {
			return DelayedClickMsg{
				ClickID: clickID,
				ItemIdx: itemIdx,
				X:       x,
				Y:       itemY,
			}
		})
	case 2:
		m.selectWord(itemIdx, x, itemY)
	case 3:
		m.selectLine(itemIdx, itemY)
		m.clickCount = 0
	}

	return true, cmd
}

// HandleDelayedClick handles a delayed single-click action.
func (m *Chat) HandleDelayedClick(msg DelayedClickMsg) bool {
	if msg.ClickID != m.pendingClickID {
		return false
	}
	if m.HasHighlight() {
		return false
	}

	if expandable, ok := m.list.SelectedItem().(chat.Expandable); ok {
		if !expandable.ToggleExpanded() {
			m.ScrollToIndex(m.list.Selected())
		}
		if m.AtBottom() {
			m.ScrollToBottom()
		}
		return true
	}

	return false
}

// HandleMouseUp handles mouse up events for the chat component.
func (m *Chat) HandleMouseUp(x, y int) bool {
	if !m.mouseDown {
		return false
	}

	m.mouseDown = false
	return true
}

// HandleMouseDrag handles mouse drag events for the chat component.
func (m *Chat) HandleMouseDrag(x, y int) bool {
	if !m.mouseDown {
		return false
	}
	if m.list.Len() == 0 {
		return false
	}

	itemIdx, itemY := m.list.ItemIndexAtPosition(x, y)
	if itemIdx < 0 {
		return false
	}

	m.mouseDragItem = itemIdx
	m.mouseDragX = x
	m.mouseDragY = itemY
	return true
}

// HasHighlight returns whether there is currently highlighted content.
func (m *Chat) HasHighlight() bool {
	startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
	return startItemIdx >= 0 && endItemIdx >= 0 && (startLine != endLine || startCol != endCol)
}

// HighlightContent returns the currently highlighted content.
func (m *Chat) HighlightContent() string {
	startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
	if startItemIdx < 0 || endItemIdx < 0 || startLine == endLine && startCol == endCol {
		return ""
	}

	var sb strings.Builder
	for i := startItemIdx; i <= endItemIdx; i++ {
		item := m.list.ItemAt(i)
		if hi, ok := item.(list.Highlightable); ok {
			startLine, startCol, endLine, endCol := hi.Highlight()
			listWidth := m.list.Width()
			var rendered string
			if rr, ok := item.(list.RawRenderable); ok {
				rendered = rr.RawRender(listWidth)
			} else {
				rendered = item.Render(listWidth)
			}
			sb.WriteString(list.HighlightContent(
				rendered,
				uv.Rect(0, 0, listWidth, lipgloss.Height(rendered)),
				startLine,
				startCol,
				endLine,
				endCol,
			))
			sb.WriteString(strings.Repeat("\n", m.list.Gap()))
		}
	}

	return strings.TrimSpace(sb.String())
}

// ClearMouse clears the current mouse interaction state.
func (m *Chat) ClearMouse() {
	m.mouseDown = false
	m.mouseDownItem = -1
	m.mouseDragItem = -1
	m.lastClickTime = time.Time{}
	m.lastClickX = 0
	m.lastClickY = 0
	m.clickCount = 0
	m.pendingClickID++
}

// applyHighlightRange applies the current highlight range to the chat items.
func (m *Chat) applyHighlightRange(idx, selectedIdx int, item list.Item) list.Item {
	if hi, ok := item.(list.Highlightable); ok {
		startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
		sLine, sCol, eLine, eCol := -1, -1, -1, -1
		if idx >= startItemIdx && idx <= endItemIdx {
			if idx == startItemIdx && idx == endItemIdx {
				sLine = startLine
				sCol = startCol
				eLine = endLine
				eCol = endCol
			} else if idx == startItemIdx {
				sLine = startLine
				sCol = startCol
				eLine = -1
				eCol = -1
			} else if idx == endItemIdx {
				sLine = 0
				sCol = 0
				eLine = endLine
				eCol = endCol
			} else {
				sLine = 0
				sCol = 0
				eLine = -1
				eCol = -1
			}
		}

		hi.SetHighlight(sLine, sCol, eLine, eCol)
		return hi.(list.Item)
	}

	return item
}

// getHighlightRange returns the current highlight range.
func (m *Chat) getHighlightRange() (startItemIdx, startLine, startCol, endItemIdx, endLine, endCol int) {
	if m.mouseDownItem < 0 {
		return -1, -1, -1, -1, -1, -1
	}

	downItemIdx := m.mouseDownItem
	dragItemIdx := m.mouseDragItem

	draggingDown := dragItemIdx > downItemIdx ||
		(dragItemIdx == downItemIdx && m.mouseDragY > m.mouseDownY) ||
		(dragItemIdx == downItemIdx && m.mouseDragY == m.mouseDownY && m.mouseDragX >= m.mouseDownX)

	if draggingDown {
		startItemIdx = downItemIdx
		startLine = m.mouseDownY
		startCol = m.mouseDownX
		endItemIdx = dragItemIdx
		endLine = m.mouseDragY
		endCol = m.mouseDragX
	} else {
		startItemIdx = dragItemIdx
		startLine = m.mouseDragY
		startCol = m.mouseDragX
		endItemIdx = downItemIdx
		endLine = m.mouseDownY
		endCol = m.mouseDownX
	}

	return startItemIdx, startLine, startCol, endItemIdx, endLine, endCol
}

// selectWord selects the word at the given position within an item.
func (m *Chat) selectWord(itemIdx, x, itemY int) {
	item := m.list.ItemAt(itemIdx)
	if item == nil {
		return
	}

	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	lines := strings.Split(rendered, "\n")
	if itemY < 0 || itemY >= len(lines) {
		return
	}

	offset := chat.MessageLeftPaddingTotal
	contentX := max(x-offset, 0)

	line := ansi.Strip(lines[itemY])
	startCol, endCol := findWordBoundaries(line, contentX)
	if startCol == endCol {
		m.mouseDown = true
		m.mouseDownItem = itemIdx
		m.mouseDownX = x
		m.mouseDownY = itemY
		m.mouseDragItem = itemIdx
		m.mouseDragX = x
		m.mouseDragY = itemY
		return
	}

	m.mouseDown = true
	m.mouseDownItem = itemIdx
	m.mouseDownX = startCol + offset
	m.mouseDownY = itemY
	m.mouseDragItem = itemIdx
	m.mouseDragX = endCol + offset
	m.mouseDragY = itemY
}

// selectLine selects the entire line at the given position within an item.
func (m *Chat) selectLine(itemIdx, itemY int) {
	item := m.list.ItemAt(itemIdx)
	if item == nil {
		return
	}

	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	lines := strings.Split(rendered, "\n")
	if itemY < 0 || itemY >= len(lines) {
		return
	}

	offset := chat.MessageLeftPaddingTotal
	lineLen := ansi.StringWidth(lines[itemY])

	m.mouseDown = true
	m.mouseDownItem = itemIdx
	m.mouseDownX = 0
	m.mouseDownY = itemY
	m.mouseDragItem = itemIdx
	m.mouseDragX = lineLen + offset
	m.mouseDragY = itemY
}

// findWordBoundaries finds the start and end column of the word at the given column.
func findWordBoundaries(line string, col int) (startCol, endCol int) {
	if line == "" || col < 0 {
		return 0, 0
	}

	i := displaywidth.StringGraphemes(line)
	for i.Next() {
	}

	lineCol := 0
	lastCol := 0
	iter := words.FromString(line)
	for iter.Next() {
		token := iter.Value()
		tokenWidth := displaywidth.String(token)

		graphemeStart := lineCol
		graphemeEnd := lineCol + tokenWidth
		lineCol += tokenWidth

		if col < graphemeStart {
			return lastCol, lastCol
		}

		lastCol = graphemeEnd

		if col >= graphemeStart && col < graphemeEnd {
			if strings.TrimSpace(token) == "" {
				return col, col
			}
			return graphemeStart, graphemeEnd
		}
	}

	return col, col
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func isWheelMouse(msg tea.MouseMsg) bool {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft, tea.MouseWheelRight:
		return true
	default:
		return false
	}
}

func isMouseClick(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseClickMsg)
	return ok
}

func isMouseMotion(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseMotionMsg)
	return ok
}

func isMouseRelease(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseReleaseMsg)
	return ok
}
