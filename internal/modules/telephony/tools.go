// Package telephony is the module that places phone calls through the
// telephony gateway.
package telephony

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	CallToolID    = "telephony_call"
	EndCallToolID = "telephony_end_call"

	healthTimeout = 2 * time.Second
)

type Module struct {
	mu      sync.RWMutex
	cfg     setup.Config
	gateway *gateway
}

func New() *Module { return &Module{} }

func (m *Module) ID() string { return setup.TelephonyModuleID }

func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	telephony := cfg.Modules.Telephony
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.gateway = nil
	if telephony.Enabled && telephony.GatewayURL != "" {
		m.gateway = newGateway(telephony.GatewayURL, telephony.GatewayToken)
	}
	return nil
}

// current is the applied config and the gateway client, nil while off.
func (m *Module) current() (setup.Config, *gateway) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg, m.gateway
}

// Tools are offered only while telephony is on with a gateway.
func (m *Module) Tools() []tools.Executor {
	if _, gw := m.current(); gw == nil {
		return nil
	}
	return []tools.Executor{&callTool{module: m}, &endCallTool{module: m}}
}

func (m *Module) Context() string { return "" }

func (m *Module) Close() error { return nil }

func (m *Module) Status(context.Context) modules.Status {
	cfg, gw := m.current()
	telephony := cfg.Modules.Telephony
	return modules.Status{
		ID:      m.ID(),
		Title:   "Telephony",
		Enabled: telephony.Enabled,
		Ready:   gw != nil,
		State:   configStatus(telephony),
		Facts: []modules.Fact{
			{Key: "gateway_configured", Label: "Gateway", Value: boolText(telephony.GatewayURL != "")},
			{Key: "token_configured", Label: "Token", Value: boolText(telephony.GatewayToken != "")},
		},
	}
}

func configStatus(cfg setup.TelephonyConfig) string {
	switch {
	case !cfg.Enabled && cfg.GatewayURL != "":
		return "Disabled"
	case !cfg.Enabled:
		return "Not configured"
	case cfg.GatewayURL == "":
		return "Gateway URL required"
	default:
		return "Configured"
	}
}

func boolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
