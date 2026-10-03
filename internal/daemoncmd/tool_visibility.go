package daemoncmd

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// setupAwareToolExecutor hides telephony_call while telephony is not set up;
// telephony_end_call stays for calls already in progress.
type setupAwareToolExecutor struct {
	core.ToolExecutor
	setup *setup.Service
}

func newSetupAwareToolExecutor(inner core.ToolExecutor, setupService *setup.Service) core.ToolExecutor {
	return &setupAwareToolExecutor{ToolExecutor: inner, setup: setupService}
}

func (e *setupAwareToolExecutor) List() []tools.Spec {
	specs := e.ToolExecutor.List()
	out := specs[:0]
	for _, spec := range specs {
		if e.visible(spec.ID) {
			out = append(out, spec)
		}
	}
	return out
}

func (e *setupAwareToolExecutor) Spec(toolID string) (tools.Spec, bool) {
	spec, ok := e.ToolExecutor.Spec(toolID)
	if !ok || !e.visible(spec.ID) {
		return tools.Spec{}, false
	}
	return spec, true
}

func (e *setupAwareToolExecutor) visible(toolID string) bool {
	switch strings.TrimSpace(toolID) {
	case telephonymodule.CallToolID:
		return e.telephonyAvailable()
	default:
		return true
	}
}

func (e *setupAwareToolExecutor) telephonyAvailable() bool {
	if e.setup == nil {
		return false
	}
	cfg, err := e.setup.Load()
	if err != nil {
		return false
	}
	module := setup.TelephonyModuleFromConfig(cfg.Modules)
	return module.Enabled && strings.TrimSpace(cfg.Modules.Telephony.GatewayURL) != ""
}
