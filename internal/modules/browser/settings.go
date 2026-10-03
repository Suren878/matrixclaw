package browser

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *Module) Settings(context.Context) []modules.Item {
	module := m.Descriptor()
	choice := modules.Item{Key: "provider", Kind: modules.ItemChoice, Label: "Browser Provider", Value: "off",
		Options: []modules.Option{{Value: "off", Label: "Disabled"}}}
	if module.Enabled {
		choice.Value = module.ProviderID
	}
	for _, provider := range module.Providers {
		choice.Options = append(choice.Options, modules.Option{Value: provider.ID, Label: provider.Name, Info: installState(provider)})
	}
	provider := selected(module)
	installed := provider.RuntimeInstalled && provider.BrowserInstalled
	engine := modules.Item{Key: "engine", Kind: modules.ItemAction, Label: "Engine", Display: installState(provider),
		Confirm: installQuestion(provider)}
	if installed {
		engine.Danger = true
		engine.Confirm = "Delete " + provider.Name + " runtime and managed Chromium files?"
	}
	return []modules.Item{
		engine,
		choice,
		{Key: "run_mode", Kind: modules.ItemChoice, Label: "Runtime Mode", Value: runMode(provider.Config.RuntimeMode),
			Options: []modules.Option{{Value: "per_task", Label: "Run Per Task"}, {Value: "always_running", Label: "Always Running"}}},
	}
}

// Change picks the provider or run mode, or installs or deletes the
// selected provider's engine (and so its MCP server).
func (m *Module) Change(ctx context.Context, path []string, value string) (modules.Change, error) {
	if len(path) != 1 {
		return modules.Change{}, modules.Unknown(path)
	}
	module := m.Descriptor()
	switch path[0] {
	case "provider":
		if value == "off" {
			return modules.Change{Config: func(cfg *setup.Config) error { cfg.Modules.Browser.Enabled = false; return nil }}, nil
		}
		for _, provider := range module.Providers {
			if provider.ID == value {
				return modules.Change{Config: func(cfg *setup.Config) error {
					cfg.Modules.Browser.Enabled = true
					cfg.Modules.Browser.ProviderID = value
					return nil
				}}, nil
			}
		}
		return modules.Change{}, fmt.Errorf("%w: unknown browser provider %q", modules.ErrInvalidSetting, value)
	case "run_mode":
		if value != "per_task" && value != "always_running" {
			return modules.Change{}, fmt.Errorf("%w: unknown run mode %q", modules.ErrInvalidSetting, value)
		}
		return modules.Change{Config: func(cfg *setup.Config) error { cfg.Modules.Browser.ProviderConfig.RuntimeMode = value; return nil }}, nil
	case "engine":
		provider := selected(module)
		action := localruntime.ActionInstallRuntime
		if provider.RuntimeInstalled && provider.BrowserInstalled {
			action = localruntime.ActionDeleteRuntime
		}
		if _, err := m.runtime.ApplyBrowserAction(ctx, provider, setup.BrowserProviderActionRequest{Action: action}); err != nil {
			return modules.Change{}, err
		}
		return modules.Change{Reload: true}, nil
	default:
		return modules.Change{}, modules.Unknown(path)
	}
}

func installState(provider setup.BrowserProviderOption) string {
	switch {
	case provider.RuntimeInstalled && provider.BrowserInstalled:
		return "Installed"
	case strings.Contains(strings.ToLower(provider.Status), "repair required"):
		return "Repair Required"
	case provider.RuntimeInstalled:
		return "Browser Missing"
	default:
		return "Not Installed"
	}
}

func installQuestion(provider setup.BrowserProviderOption) string {
	if provider.RuntimeInstalled && !provider.BrowserInstalled {
		return "Download the managed Chromium browser required by " + provider.Name + "?"
	}
	return "Download " + provider.Name + " runtime and managed Chromium?"
}

func runMode(mode string) string {
	if mode == "always_running" {
		return mode
	}
	return "per_task"
}
