package daemoncmd

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestVoiceStatusLeavesOutTheRuntimeState(t *testing.T) {
	module := setup.VoiceModuleDescriptor{ID: setup.VoiceModuleTTS, Enabled: true, ProviderID: "local",
		Providers: []setup.VoiceProviderOption{{ID: "local", Name: "Local", RuntimeState: "running"}}}
	running := voiceStatusLine(module, nil)
	module.Providers[0].RuntimeState = "stopped"
	if stopped := voiceStatusLine(module, nil); stopped != running || strings.Contains(running, "running") {
		t.Fatalf("status lines differ by runtime state: %q / %q", running, stopped)
	}
}
