package daemoncmd

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type setupAwareToolExecutor struct {
	inner core.ToolExecutor
	setup *setup.Service
}

func newSetupAwareToolExecutor(inner core.ToolExecutor, setupService *setup.Service) core.ToolExecutor {
	if inner == nil {
		return nil
	}
	return &setupAwareToolExecutor{inner: inner, setup: setupService}
}

func (e *setupAwareToolExecutor) List() []tools.Spec {
	if e == nil || e.inner == nil {
		return nil
	}
	specs := e.inner.List()
	out := specs[:0]
	for _, spec := range specs {
		if e.visible(spec.ID) {
			out = append(out, spec)
		}
	}
	return out
}

func (e *setupAwareToolExecutor) Spec(toolID string) (tools.Spec, bool) {
	if e == nil || e.inner == nil {
		return tools.Spec{}, false
	}
	spec, ok := e.inner.Spec(toolID)
	if !ok || !e.visible(spec.ID) {
		return tools.Spec{}, false
	}
	return spec, true
}

func (e *setupAwareToolExecutor) Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error) {
	if e == nil || e.inner == nil {
		return tools.Result{}, core.ErrExecutionUnavailable
	}
	return e.inner.Execute(ctx, toolID, call)
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
	if e == nil || e.setup == nil {
		return false
	}
	cfg, err := e.setup.Load()
	if err != nil {
		return false
	}
	module := setup.TelephonyModuleFromConfig(cfg.Modules)
	return module.Enabled && strings.TrimSpace(cfg.Modules.Telephony.GatewayURL) != ""
}
