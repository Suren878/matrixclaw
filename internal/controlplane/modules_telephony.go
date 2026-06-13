package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleTelephonyModule(ctx context.Context, args string) (Result, error) {
	if d.telephony == nil {
		return unsupportedRuntime("telephony"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.telephonyPicker(ctx)
	case "enabled":
		return d.telephonyEnabledPicker(ctx)
	case "set-enabled":
		return d.setTelephonyEnabled(ctx, rest)
	case "field":
		return d.telephonyFieldPrompt(ctx, rest)
	case "set":
		return d.telephonySetField(ctx, rest)
	case "info", "status":
		return d.telephonyInfo(ctx)
	default:
		return d.telephonyPicker(ctx)
	}
}

func (d *Dispatcher) telephonyPicker(ctx context.Context) (Result, error) {
	module, err := d.telephony.TelephonyModule(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerTelephony, module.Title).
			Context(module.ID).
			Back(modulesCommand()).
			Row("enabled", "Enabled", formatEnabled(module.Enabled), telephonyCommand("enabled")).
			Row("gateway", "Gateway URL", telephonyGatewayURLStatus(module), telephonyCommand("field", "gateway-url")).
			Row("profile", "Default Profile", telephonyProfileStatus(module), telephonyCommand("field", "profile")).
			Row("phone-prompt", "Phone Prompt", telephonyPhonePromptStatus(module), telephonyCommand("field", "phone-prompt")).
			Row("token", "Gateway Token", telephonyTokenStatus(module), telephonyCommand("field", "token")).
			Row("status", "Status", module.Status, telephonyCommand("info")).
			Ptr(),
	}, nil
}

func (d *Dispatcher) telephonyEnabledPicker(ctx context.Context) (Result, error) {
	module, err := d.telephony.TelephonyModule(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerTelephony, "Telephony Enabled").
			Context(module.ID).
			Meta(module.Status).
			Select(telephonyCommand()).
			Item(PickerItem{ID: "on", Title: "On", Info: "Use telephony gateway for calls", Selected: module.Enabled, Disabled: strings.TrimSpace(module.GatewayURL) == "", Command: telephonyCommand("set-enabled", "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Info: "Calls disabled", Selected: !module.Enabled, Command: telephonyCommand("set-enabled", "off")}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setTelephonyEnabled(ctx context.Context, value string) (Result, error) {
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		enabled = true
	case "no", "off", "false", "disable", "disabled":
		enabled = false
	default:
		return d.telephonyEnabledPicker(ctx)
	}
	if _, err := d.telephony.UpdateTelephonyModule(ctx, setup.TelephonyModuleUpdate{Enabled: &enabled}); err != nil {
		return Result{}, err
	}
	return d.telephonyPicker(ctx)
}

func (d *Dispatcher) telephonyFieldPrompt(ctx context.Context, field string) (Result, error) {
	module, err := d.telephony.TelephonyModule(ctx)
	if err != nil {
		return Result{}, err
	}
	title, placeholder, value, sensitive := telephonyPrompt(field, module)
	if title == "" {
		return d.telephonyPicker(ctx)
	}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               title,
		Placeholder:         placeholder,
		Value:               value,
		SubmitCommandPrefix: telephonyCommandPrefix("set", field),
		CancelCommand:       telephonyCommand(),
		Sensitive:           sensitive,
	}}, nil
}

func (d *Dispatcher) telephonySetField(ctx context.Context, args string) (Result, error) {
	field, value := firstCommandStep(args)
	value = strings.TrimSpace(value)
	update := setup.TelephonyModuleUpdate{}
	switch normalizeTelephonyField(field) {
	case "gateway-url":
		update.GatewayURL = value
	case "profile":
		update.DefaultProfile = value
	case "phone-prompt":
		update.PhonePrompt = value
	case "token":
		update.GatewayToken = value
		update.ClearToken = value == "-"
	default:
		return d.telephonyPicker(ctx)
	}
	if _, err := d.telephony.UpdateTelephonyModule(ctx, update); err != nil {
		return Result{}, err
	}
	return d.telephonyPicker(ctx)
}

func (d *Dispatcher) telephonyInfo(ctx context.Context) (Result, error) {
	module, err := d.telephony.TelephonyModule(ctx)
	if err != nil {
		return Result{}, err
	}
	rows := []InfoRow{
		{Label: "Enabled", Value: formatEnabled(module.Enabled)},
		{Label: "Status", Value: module.Status},
		{Label: "Gateway", Value: telephonyGatewayURLStatus(module)},
		{Label: "Gateway Reachable", Value: formatEnabled(module.GatewayReachable)},
		{Label: "Token", Value: telephonyTokenStatus(module)},
		{Label: "Default Profile", Value: telephonyProfileStatus(module)},
		{Label: "Phone Prompt", Value: telephonyPhonePromptStatus(module)},
		{Label: "Realtime Module", Value: module.RealtimeModuleID},
	}
	if strings.TrimSpace(module.GatewayError) != "" {
		rows = append(rows, InfoRow{Label: "Gateway Error", Value: module.GatewayError})
	}
	return Result{Handled: true, Info: &InfoData{Title: module.Title + " Status", Rows: rows}}, nil
}

func telephonyPrompt(field string, module setup.TelephonyModuleDescriptor) (title string, placeholder string, value string, sensitive bool) {
	switch normalizeTelephonyField(field) {
	case "gateway-url":
		return "Telephony Gateway URL", "http://127.0.0.1:8090", module.GatewayURL, false
	case "profile":
		return "Default Call Profile", "main", module.DefaultProfile, false
	case "phone-prompt":
		return "Phone Model Prompt", "How the assistant should behave during real phone calls", module.Config.PhonePrompt, false
	case "token":
		return "Telephony Gateway Token", firstNonEmptyTrimmed(module.TokenPreview, "optional bearer token"), "", true
	default:
		return "", "", "", false
	}
}

func normalizeTelephonyField(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "gateway", "gateway-url", "gateway_url", "url", "endpoint":
		return "gateway-url"
	case "profile", "default-profile", "default_profile":
		return "profile"
	case "phone-prompt", "phone_prompt", "prompt", "call-prompt", "call_prompt":
		return "phone-prompt"
	case "token", "gateway-token", "gateway_token":
		return "token"
	default:
		return ""
	}
}

func telephonyGatewayURLStatus(module setup.TelephonyModuleDescriptor) string {
	if strings.TrimSpace(module.GatewayURL) == "" {
		return "Required"
	}
	if module.GatewayReachable {
		return module.GatewayURL + " · reachable"
	}
	if module.GatewayError != "" {
		return module.GatewayURL + " · unreachable"
	}
	return module.GatewayURL
}

func telephonyProfileStatus(module setup.TelephonyModuleDescriptor) string {
	if strings.TrimSpace(module.DefaultProfile) == "" {
		return "Gateway default"
	}
	return module.DefaultProfile
}

func telephonyTokenStatus(module setup.TelephonyModuleDescriptor) string {
	if module.TokenConfigured {
		return firstNonEmptyTrimmed(module.TokenPreview, "Configured")
	}
	return "Not set"
}

func telephonyPhonePromptStatus(module setup.TelephonyModuleDescriptor) string {
	if strings.TrimSpace(module.Config.PhonePrompt) == "" {
		return "Not set"
	}
	return "Configured"
}

func telephonyModuleListInfo(module setup.TelephonyModuleDescriptor) string {
	if !module.Enabled {
		return ""
	}
	if module.GatewayReachable {
		return "Gateway ready"
	}
	return module.Status
}
