package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type testMessageItem struct {
	id   string
	body string
}

func (m testMessageItem) ID() string {
	return m.id
}

func (m testMessageItem) Render(int) string {
	return m.body
}

func (m testMessageItem) RawRender(int) string {
	return m.body
}

func newTestChat(t *testing.T, height int, msgs ...chat.MessageItem) *Chat {
	t.Helper()
	sty := styles.DefaultStyles()
	c := NewChat(&common.Common{Styles: &sty})
	c.SetSize(20, height)
	c.SetMessages(msgs...)
	return c
}

func testMessages(count int) []chat.MessageItem {
	msgs := make([]chat.MessageItem, 0, count)
	for i := 1; i <= count; i++ {
		msgs = append(msgs, testMessageItem{
			id:   fmt.Sprintf("msg-%02d", i),
			body: strings.Join([]string{fmt.Sprintf("msg-%02d-a", i), fmt.Sprintf("msg-%02d-b", i)}, "\n"),
		})
	}
	return msgs
}

func TestChatInitialMessagesFollowBottom(t *testing.T) {
	c := newTestChat(t, 3, testMessages(5)...)

	if !c.Follow() {
		t.Fatalf("Follow() = false after initial messages, want true")
	}
	if !c.AtBottom() {
		t.Fatalf("AtBottom() = false after initial messages, want true")
	}
}

func TestChatScrollUpDisablesFollow(t *testing.T) {
	c := newTestChat(t, 3, testMessages(5)...)

	c.ScrollBy(-2)

	if c.Follow() {
		t.Fatalf("Follow() = true after scrolling up, want false")
	}
	if c.AtBottom() {
		t.Fatalf("AtBottom() = true after scrolling up, want false")
	}
}

func TestChatScrollDownEnablesFollowOnlyAtBottom(t *testing.T) {
	c := newTestChat(t, 3, testMessages(6)...)
	c.ScrollBy(-6)
	if c.Follow() {
		t.Fatalf("test setup left follow enabled")
	}

	c.ScrollBy(1)
	if c.Follow() {
		t.Fatalf("Follow() = true before reaching bottom, want false")
	}

	c.ScrollBy(100)
	if !c.Follow() {
		t.Fatalf("Follow() = false after reaching bottom, want true")
	}
	if !c.AtBottom() {
		t.Fatalf("AtBottom() = false after reaching bottom, want true")
	}
}

func TestChatResizeKeepsBottomWhenFollowing(t *testing.T) {
	c := newTestChat(t, 4, testMessages(6)...)
	before := c.View()

	c.SetSize(20, 2)

	if !c.Follow() {
		t.Fatalf("Follow() = false after resizing while following, want true")
	}
	if !c.AtBottom() {
		t.Fatalf("AtBottom() = false after resizing while following, want true")
	}
	if got := c.View(); got == before {
		t.Fatalf("View() did not change after shrinking viewport; test setup is ineffective")
	}
	if !strings.Contains(c.View(), "msg-06") {
		t.Fatalf("View() after resize does not include last message:\n%s", c.View())
	}
}

func TestChatResizePreservesViewportWhenNotFollowing(t *testing.T) {
	c := newTestChat(t, 4, testMessages(8)...)
	c.ScrollBy(-5)
	before := c.View()

	c.SetSize(20, 4)

	if c.Follow() {
		t.Fatalf("Follow() = true after resizing above bottom, want false")
	}
	if got := c.View(); got != before {
		t.Fatalf("View() after same-size resize =\n%s\nwant\n%s", got, before)
	}
}

func TestChatSetMessagesPreservesViewportWhenNotFollowing(t *testing.T) {
	msgs := testMessages(7)
	c := newTestChat(t, 4, msgs...)
	c.ScrollBy(-5)
	before := c.View()

	msgs = append(msgs, testMessageItem{id: "msg-08", body: "msg-08-a\nmsg-08-b"})
	c.SetMessages(msgs...)

	if c.Follow() {
		t.Fatalf("Follow() = true after SetMessages while above bottom, want false")
	}
	if got := c.View(); got != before {
		t.Fatalf("View() after SetMessages =\n%s\nwant\n%s", got, before)
	}
}
