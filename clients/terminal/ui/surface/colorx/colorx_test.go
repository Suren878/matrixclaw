package colorx

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

func TestHexReturnsEmptyForNoColor(t *testing.T) {
	if got := Hex(lipgloss.NoColor{}); got != "" {
		t.Fatalf("Hex(NoColor) = %q, want empty", got)
	}
}

func TestHexUsesCharmtoneHex(t *testing.T) {
	if got := Hex(charmtone.Guac); got != charmtone.Guac.Hex() {
		t.Fatalf("Hex(charmtone.Guac) = %q, want %q", got, charmtone.Guac.Hex())
	}
}
