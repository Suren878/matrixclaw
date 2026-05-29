package list

import (
	"fmt"
	"strings"
	"testing"
)

type testItem string

func (i testItem) Render(int) string {
	return string(i)
}

func TestViewportSnapshotRestorePreservesOffsetAfterItemsChange(t *testing.T) {
	l := NewList(
		testItem("one\none"),
		testItem("two\ntwo"),
		testItem("three\nthree"),
		testItem("four\nfour"),
		testItem("five\nfive"),
	)
	l.SetSize(20, 3)
	l.ScrollToBottom()
	l.ScrollBy(-3)
	if l.AtBottom() {
		t.Fatalf("test setup left list at bottom")
	}
	before := l.Render()
	snapshot := l.SnapshotViewport()

	l.SetItems(
		testItem("one\none"),
		testItem("two\ntwo"),
		testItem("three\nthree"),
		testItem("four\nfour"),
		testItem("five\nfive"),
		testItem("six\nsix"),
	)
	l.RestoreViewport(snapshot)

	if got := l.Render(); got != before {
		t.Fatalf("Render() after restore =\n%s\nwant\n%s", got, before)
	}
	if l.AtBottom() {
		t.Fatalf("AtBottom() = true after restore above bottom, want false")
	}
}

func TestViewportRestoreClampsToBottomWhenListShrinks(t *testing.T) {
	l := NewList(
		testItem("one"),
		testItem("two"),
		testItem("three"),
		testItem("four"),
		testItem("five"),
	)
	l.SetSize(20, 2)
	l.ScrollToBottom()
	snapshot := l.SnapshotViewport()

	l.SetItems(testItem("one"), testItem("two"))
	l.RestoreViewport(snapshot)

	if !l.AtBottom() {
		t.Fatalf("AtBottom() = false after clamped restore, want true")
	}
	if got, want := l.Render(), strings.Join([]string{"one", "two"}, "\n"); got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestViewportRestoreAllowsOffsetIntoGap(t *testing.T) {
	l := NewList(testItem("one\none"), testItem("two\ntwo"), testItem("three\nthree"))
	l.SetGap(1)
	l.SetSize(20, 2)
	l.ScrollToBottom()
	l.ScrollBy(-2)
	before := l.Render()
	if before == "" {
		t.Fatalf("test setup rendered empty viewport")
	}
	snapshot := l.SnapshotViewport()

	l.SetItems(testItem("one\none"), testItem("two\ntwo"), testItem("three\nthree"), testItem("four\nfour"))
	l.RestoreViewport(snapshot)

	if got := l.Render(); got != before {
		t.Fatalf("Render() after restoring gap offset = %q, want %q (snapshot=%s)", got, before, fmt.Sprintf("%+v", snapshot))
	}
}
