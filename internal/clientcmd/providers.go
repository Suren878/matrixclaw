package clientcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runProvidersCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	if len(args) > 0 && strings.TrimSpace(args[0]) == "verify" {
		return runProviderVerifyCommand(stdout, stderr, binaryName, service)
	}
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "providers", err)
	}
	items := appsetup.ProviderSetupItemsFromConfig(cfg, service.ProviderOptions())
	for _, item := range items {
		state := "available"
		if item.Configured {
			state = "configured"
		}
		model := strings.TrimSpace(item.Model)
		if model == "" {
			model = strings.TrimSpace(item.DefaultModel)
		}
		if model != "" {
			fmt.Fprintf(stdout, "%s: %s [%s] %s\n", binaryName, item.Name, state, model)
			continue
		}
		fmt.Fprintf(stdout, "%s: %s [%s]\n", binaryName, item.Name, state)
	}
	return 0
}

func runProviderVerifyCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service) int {
	draft, err := service.Draft()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "providers verify", err)
	}
	configured := appsetup.ConfiguredProviders(draft)
	if len(configured) == 0 {
		fmt.Fprintf(stdout, "%s: providers verify: no configured providers\n", binaryName)
		return 1
	}
	cfg, _ := service.Load()
	failures := 0
	for _, provider := range configured {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = strings.TrimSpace(provider.ID)
		}
		if !providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
			ProviderID:   firstNonEmptyTrimmed(provider.CatalogID, provider.ID),
			ProviderType: provider.Type,
			ModelID:      provider.Model,
		}).ProviderCapabilities.ModelDiscovery {
			fmt.Fprintf(stdout, "%s: provider %s: skipped (model discovery unsupported)\n", binaryName, name)
			continue
		}
		models, err := service.ProviderModels(context.Background(), provider)
		if err != nil {
			fmt.Fprintf(stdout, "%s: provider %s: ERROR %s\n", binaryName, name, redactSecrets(err.Error(), provider.APIKey, providerSecret(cfg, provider.ID)))
			failures++
			continue
		}
		fmt.Fprintf(stdout, "%s: provider %s: ok (%d models)\n", binaryName, name, len(models))
	}
	if failures > 0 {
		fmt.Fprintf(stdout, "%s: providers verify: failed (%d issue(s))\n", binaryName, failures)
		return 1
	}
	fmt.Fprintf(stdout, "%s: providers verify: ok\n", binaryName)
	return 0
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func providerSecret(cfg appsetup.Config, providerID string) string {
	providerID = strings.TrimSpace(providerID)
	for _, provider := range cfg.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.ID), providerID) {
			return strings.TrimSpace(provider.APIKey)
		}
	}
	return ""
}
