package dialog

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type loadingTickMsg struct{}

var loadingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}

func loadingTickCmd() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg {
		return loadingTickMsg{}
	})
}

func loadingFrame(frame int) string {
	if len(loadingFrames) == 0 {
		return ""
	}
	return loadingFrames[frame%len(loadingFrames)]
}

func loadingTitle(title string, loading bool, frame int) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Loading"
	}
	if !loading {
		return title
	}
	return title + " " + loadingFrame(frame)
}
