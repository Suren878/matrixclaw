package planview

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestTreeGuidesAlignChildrenUnderParentMarker(t *testing.T) {
	items := []core.PlanItem{
		{ID: "parent", Text: "Parent"},
		{ID: "child-1", ParentID: "parent", Text: "Child"},
		{ID: "child-2", ParentID: "parent", Text: "Child"},
	}

	guides := TreeGuides(items)

	if got := guides["parent"]; got != "" {
		t.Fatalf("parent guide = %q, want empty", got)
	}
	if got := guides["child-1"]; got != " ├─ " {
		t.Fatalf("first child guide = %q, want centered branch", got)
	}
	if got := guides["child-2"]; got != " └─ " {
		t.Fatalf("last child guide = %q, want centered branch", got)
	}
}

func TestTopLevelContinuationDoesNotDrawGuideBetweenTasks(t *testing.T) {
	got := ContinuationPrefix("  ", "", "[✓]")
	if got != "        " {
		t.Fatalf("top continuation = %q, want spaces only", got)
	}
}

func TestChildContinuationKeepsGroupGuide(t *testing.T) {
	got := ContinuationPrefix("  ", " ├─ ", "[✓]")
	if got != "   │      " {
		t.Fatalf("child continuation = %q, want group guide", got)
	}
}
