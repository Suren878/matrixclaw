package telephony

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// Settings are the gateway settings and, probed now, the gateway's health.
func (m *Module) Settings(ctx context.Context) []modules.Item {
	cfg, _ := m.current()
	telephony := cfg.Modules.Telephony
	state, facts := m.probe(ctx, telephony)
	return []modules.Item{
		{Key: "enabled", Kind: modules.ItemToggle, Label: "Enabled", Value: modules.OnOff(telephony.Enabled),
			Disabled: !telephony.Enabled && telephony.GatewayURL == "", Hint: "Set the gateway URL first"},
		{Key: "gateway_url", Kind: modules.ItemText, Label: "Gateway URL", Value: telephony.GatewayURL,
			Display: firstNonEmpty(telephony.GatewayURL, "Required"), Hint: "http://127.0.0.1:8090"},
		{Key: "default_profile", Kind: modules.ItemText, Label: "Default Profile", Value: telephony.DefaultProfile,
			Display: firstNonEmpty(telephony.DefaultProfile, "Gateway default"), Hint: "main"},
		{Key: "phone_prompt", Kind: modules.ItemText, Label: "Phone Prompt", Value: telephony.PhonePrompt,
			Display: setOrNot(telephony.PhonePrompt != "", "Configured"), Hint: "How the assistant should behave during real phone calls"},
		{Key: "gateway_token", Kind: modules.ItemSecret, Label: "Gateway Token",
			Display: setOrNot(telephony.GatewayToken != "", setup.MaskSecret(telephony.GatewayToken)), Hint: "optional bearer token"},
		{Key: "status", Kind: modules.ItemInfo, Label: "Status", Display: state, Facts: facts},
	}
}

// probe asks the gateway for its health; the state names what is wrong.
func (m *Module) probe(ctx context.Context, telephony setup.TelephonyConfig) (string, []modules.Fact) {
	state := configStatus(telephony)
	facts := []modules.Fact{{Key: "gateway", Label: "Gateway", Value: firstNonEmpty(telephony.GatewayURL, "Not set")}}
	if telephony.GatewayURL == "" {
		return state, facts
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	health, err := newGateway(telephony.GatewayURL, telephony.GatewayToken).Health(ctx)
	switch {
	case err != nil:
		facts = append(facts, modules.Fact{Key: "error", Label: "Gateway error", Value: err.Error()})
		if telephony.Enabled {
			state = "Gateway unreachable"
		}
	case !health.Ready:
		facts = append(facts, modules.Fact{Key: "error", Label: "Gateway error", Value: "gateway is not ready: " + firstNonEmpty(health.Error, "no reason given")})
		if telephony.Enabled {
			state = "Gateway degraded"
		}
	case telephony.Enabled:
		state = "Ready"
	}
	return state, facts
}

func (m *Module) Change(_ context.Context, path []string, value string) (modules.Change, error) {
	if len(path) != 1 {
		return modules.Change{}, modules.Unknown(path)
	}
	var edit func(*setup.TelephonyConfig) error
	switch path[0] {
	case "enabled":
		enabled, err := modules.Toggle(value)
		if err != nil {
			return modules.Change{}, err
		}
		edit = func(t *setup.TelephonyConfig) error { t.Enabled = enabled; return nil }
	case "gateway_url":
		edit = func(t *setup.TelephonyConfig) error { t.GatewayURL = value; return nil }
	case "default_profile":
		edit = func(t *setup.TelephonyConfig) error { t.DefaultProfile = value; return nil }
	case "phone_prompt":
		edit = func(t *setup.TelephonyConfig) error { t.PhonePrompt = value; return nil }
	case "gateway_token":
		edit = func(t *setup.TelephonyConfig) error { t.GatewayToken = value; return nil }
	default:
		return modules.Change{}, modules.Unknown(path)
	}
	return modules.Change{Config: func(cfg *setup.Config) error {
		if err := edit(&cfg.Modules.Telephony); err != nil {
			return err
		}
		return validate(cfg.Modules.Telephony)
	}}, nil
}

func validate(cfg setup.TelephonyConfig) error {
	gatewayURL := strings.TrimSpace(cfg.GatewayURL)
	if gatewayURL == "" {
		if cfg.Enabled {
			return fmt.Errorf("%w: telephony gateway URL is required", modules.ErrInvalidSetting)
		}
		return nil
	}
	parsed, err := url.Parse(gatewayURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: telephony gateway URL must start with http:// or https://", modules.ErrInvalidSetting)
	}
	return nil
}

func setOrNot(set bool, display string) string {
	if set {
		return display
	}
	return "Not set"
}
