package controlplane

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestBrowserRuntimeInstallInfoReportsRepairRequired(t *testing.T) {
	provider := setup.BrowserProviderOption{
		RuntimeInstalled: true,
		BrowserInstalled: false,
		Status:           "Local · browser repair required",
	}

	if got := browserRuntimeInstallInfo(provider); got != "Repair Required" {
		t.Fatalf("browserRuntimeInstallInfo = %q, want Repair Required", got)
	}
}
