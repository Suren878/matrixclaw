package components

import "strings"

type TextViewData struct {
	Title string
	Text  string
	Error string
}

func RenderTextViewCard(frame Frame, data TextViewData) string {
	frame = frame.WithInnerWidth(0)
	body := renderedTextBlock(frame, data.Text)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Body:  body,
		Error: data.Error,
	})
}

func TextViewEditorHeight(frame Frame) int {
	frame = frame.WithInnerWidth(0)
	return max(3, textViewVisibleLines(frame, ""))
}

func renderedTextBlock(frame Frame, text string) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, truncateLine(line, frame.InnerWidth()))
	}
	return out
}

func textViewVisibleLines(frame Frame, help string) int {
	if frame.Height <= 0 {
		return 12
	}
	fixed := frame.styles().Card.GetVerticalFrameSize()
	fixed += 1 // title
	fixed += 1 // title divider
	fixed += 1 // body top gap
	if strings.TrimSpace(help) != "" {
		fixed += 2 // blank line plus help
	}
	return max(3, frame.Height-fixed)
}
