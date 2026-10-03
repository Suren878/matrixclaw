package ids

import (
	"strings"
	"testing"
)

func TestNewIsPrefixedAndUnique(t *testing.T) {
	a, b := New("run"), New("run")
	if !strings.HasPrefix(a, "run_") || len(a) != len("run_")+16 {
		t.Fatalf("New = %q", a)
	}
	if a == b {
		t.Fatalf("New returned %q twice", a)
	}
}
