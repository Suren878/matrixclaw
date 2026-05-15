package planview

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/core"
)

func WrapLine(text string, width int) []string {
	wrapped := strings.TrimSpace(ansi.Wrap(text, max(1, width), " \t/\\._:=,;|-"))
	if wrapped == "" {
		return nil
	}
	return strings.Split(wrapped, "\n")
}

func ContinuationPrefix(prefix string, guide string, marker string) string {
	prefixContinuation := strings.Repeat(" ", ansi.StringWidth(prefix))
	guideContinuation := strings.Repeat(" ", ansi.StringWidth(guide))
	switch {
	case strings.TrimSpace(guide) == "":
		guideContinuation = strings.Repeat(" ", ansi.StringWidth(" │"))
	case strings.HasSuffix(guide, "├─ "):
		guideContinuation = strings.TrimSuffix(guide, "├─ ") + "│  "
	case strings.HasSuffix(guide, "└─ "):
		guideContinuation = strings.TrimSuffix(guide, "└─ ") + "   "
	}
	return prefixContinuation + guideContinuation + strings.Repeat(" ", ansi.StringWidth(marker)+1)
}

func TreeGuides(items []core.PlanItem) map[string]string {
	guides := make(map[string]string, len(items))
	if len(items) == 0 {
		return guides
	}
	children := make(map[string][]core.PlanItem, len(items))
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		ids[item.ID] = struct{}{}
	}
	for _, item := range items {
		parentID := strings.TrimSpace(item.ParentID)
		if parentID != "" {
			if _, ok := ids[parentID]; !ok {
				parentID = ""
			}
		}
		children[parentID] = append(children[parentID], item)
	}
	var walk func(parentID string, prefix string)
	walk = func(parentID string, prefix string) {
		group := children[parentID]
		for i, item := range group {
			isLast := i == len(group)-1
			if parentID == "" {
				guides[item.ID] = ""
			} else if isLast {
				guides[item.ID] = prefix + "└─ "
			} else {
				guides[item.ID] = prefix + "├─ "
			}
			nextPrefix := prefix
			if parentID == "" {
				nextPrefix = " "
			} else {
				if isLast {
					nextPrefix += "   "
				} else {
					nextPrefix += "│  "
				}
			}
			walk(item.ID, nextPrefix)
		}
	}
	walk("", "")
	return guides
}

func CompletedCount(items []core.PlanItem) int {
	done := 0
	for _, item := range items {
		if item.Status == core.PlanItemDone {
			done++
		}
	}
	return done
}

func Marker(status core.PlanItemStatus) string {
	switch status {
	case core.PlanItemDone:
		return "[✓]"
	case core.PlanItemSkipped:
		return "[x]"
	default:
		return "[•]"
	}
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
