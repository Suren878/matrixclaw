package list

// ScrollToTop scrolls the list to the top.
func (l *List) ScrollToTop() {
	l.offsetIdx = 0
	l.offsetLine = 0
}

// ScrollToBottom scrolls the list to the bottom.
func (l *List) ScrollToBottom() {
	if len(l.items) == 0 {
		return
	}

	lastOffsetIdx, lastOffsetLine, _ := l.lastOffsetItem()
	l.offsetIdx = lastOffsetIdx
	l.offsetLine = lastOffsetLine
}

// ScrollToSelected scrolls the list to the selected item.
func (l *List) ScrollToSelected() {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return
	}

	startIdx, endIdx := l.VisibleItemIndices()
	if l.selectedIdx < startIdx {
		l.offsetIdx = l.selectedIdx
		l.offsetLine = 0
		return
	}
	if l.selectedIdx <= endIdx {
		return
	}

	var totalHeight int
	for i := l.selectedIdx; i >= 0; i-- {
		item := l.getItem(i)
		totalHeight += item.height
		if l.gap > 0 && i < l.selectedIdx {
			totalHeight += l.gap
		}
		if totalHeight >= l.height {
			l.offsetIdx = i
			l.offsetLine = totalHeight - l.height
			break
		}
	}
	if totalHeight < l.height {
		l.ScrollToTop()
	}
}
