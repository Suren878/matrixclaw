package commandui

type SearchListData struct {
	Title             string
	Meta              string
	SearchValue       string
	SearchPlaceholder string
	SearchActive      bool
	EmptyText         string
	TopItems          []Item
	TopSelected       int
	Items             []Item
	Selected          int
	Footer            []Item
	FooterSelected    int
	Help              string
	Error             string
}

func RenderSearchListCard(frame Frame, data SearchListData) string {
	shortcutItems := append(append([]Item{}, data.TopItems...), data.Items...)
	shortcutItems = append(shortcutItems, data.Footer...)
	help := helpWithShortcuts(firstNonEmpty(data.Help, "type search · enter select · ↑/↓ move · esc back"), shortcutItems)
	frame = frame.WithInnerWidth(0)
	body := renderItemLines(frame, data.TopItems, data.TopSelected)
	if len(body) > 0 {
		body = append(body, "")
	}
	body = append(body, RenderTextField(frame, TextFieldData{
		Value:       data.SearchValue,
		Placeholder: firstNonEmpty(data.SearchPlaceholder, "Search"),
		Active:      data.SearchActive,
	}), "")
	if selectableCount(data.Items) == 0 {
		body = append(body, RenderTextField(frame, TextFieldData{Value: firstNonEmpty(data.EmptyText, "No items match")}), "")
	}
	body = append(body, renderItemsWithFooter(frame, data.Items, data.Selected, nil, data.Footer, data.FooterSelected)...)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Meta:  data.Meta,
		Body:  body,
		Help:  help,
		Error: data.Error,
	})
}
