package model

import "testing"

type fakeMessageItem struct {
	id       string
	rendered string
	raw      string
}

func (f fakeMessageItem) ID() string              { return f.id }
func (f fakeMessageItem) Render(width int) string { return f.rendered }
func (f fakeMessageItem) RawRender(width int) string {
	return f.raw
}

type renderOnlyItem struct {
	rendered string
}

func (r renderOnlyItem) Render(width int) string {
	return r.rendered
}

func TestCopyContentUsesRawRender(t *testing.T) {
	model := NewChat(nil)
	model.SetSize(80, 5)
	model.SetMessages(fakeMessageItem{
		id:       "msg-1",
		rendered: "\x1b[31mrendered\x1b[0m",
		raw:      "raw content",
	})
	model.SetSelected(0)

	if got := model.CopyContent(); got != "raw content" {
		t.Fatalf("CopyContent() = %q, want raw content", got)
	}
}

func TestRenderListItemContentFallsBackToRender(t *testing.T) {
	item := renderOnlyItem{rendered: "rendered content"}

	if got := renderListItemContent(item, 80); got != "rendered content" {
		t.Fatalf("renderListItemContent() = %q, want rendered content", got)
	}
}
