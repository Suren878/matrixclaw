package telephony

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const CallToolID = "telephony_call"
const EndCallToolID = "telephony_end_call"

type setupLoader interface {
	Load() (setup.Config, error)
}

type gatewayTool struct {
	setup setupLoader
	http  *http.Client
}

type callTool struct {
	gateway gatewayTool
}

type endCallTool struct {
	gateway gatewayTool
}

func NewCallTool(setupService setupLoader) tools.Executor {
	return &callTool{gateway: newGatewayTool(setupService, 20*time.Second)}
}

func NewEndCallTool(setupService setupLoader) tools.Executor {
	return &endCallTool{gateway: newGatewayTool(setupService, 10*time.Second)}
}

func newGatewayTool(setupService setupLoader, timeout time.Duration) gatewayTool {
	return gatewayTool{
		setup: setupService,
		http:  &http.Client{Timeout: timeout},
	}
}

func (t gatewayTool) config() (setup.Config, error) {
	return telephonyConfig(t.setup)
}

func telephonyConfig(setupService setupLoader) (setup.Config, error) {
	if setupService == nil {
		return setup.Config{}, fmt.Errorf("telephony setup is not configured")
	}
	cfg, err := setupService.Load()
	if err != nil {
		return setup.Config{}, err
	}
	module := setup.TelephonyModuleFromConfig(cfg.Modules)
	if !module.Enabled {
		return setup.Config{}, fmt.Errorf("telephony module is disabled")
	}
	if strings.TrimSpace(cfg.Modules.Telephony.GatewayURL) == "" {
		return setup.Config{}, fmt.Errorf("telephony gateway URL is not configured")
	}
	return cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
