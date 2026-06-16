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
	if len(args) > 0 && strings.TrimSpace(args[0]) == "login" {
		return runProviderLoginCommand(stdout, stderr, binaryName, service, args[1:])
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) == "verify" {
		return runProviderVerifyCommand(stdout, stderr, binaryName, service, args[1:])
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
			_, _ = fmt.Fprintf(stdout, "%s: %s [%s] %s\n", binaryName, item.Name, state, model)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s: %s [%s]\n", binaryName, item.Name, state)
	}
	return 0
}

func runProviderVerifyCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	draft, err := service.Draft()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "providers verify", err)
	}
	includeCatalogs := providerVerifyIncludesCatalogs(args)
	items := []appsetup.ProviderSetupItem(nil)
	if includeCatalogs {
		items, err = service.ProviderSetupItems()
		if err != nil {
			return handleSetupReadError(stderr, binaryName, service, "providers verify", err)
		}
	} else {
		configured := appsetup.ConfiguredProviders(draft)
		for _, provider := range configured {
			items = append(items, appsetup.ProviderSetupItem{
				ID:         provider.ID,
				CatalogID:  provider.CatalogID,
				Name:       provider.Name,
				Type:       provider.Type,
				Model:      provider.Model,
				Configured: true,
			})
		}
	}
	if len(items) == 0 {
		_, _ = fmt.Fprintf(stdout, "%s: providers verify: no configured providers\n", binaryName)
		return 1
	}
	cfg, _ := service.Load()
	failures := 0
	for _, provider := range items {
		if !includeCatalogs && !provider.Configured {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = strings.TrimSpace(provider.ID)
		}
		if !providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
			ProviderID:   firstNonEmptyTrimmed(provider.CatalogID, provider.ID),
			ProviderType: provider.Type,
			ModelID:      provider.Model,
		}).ProviderCapabilities.ModelDiscovery {
			_, _ = fmt.Fprintf(stdout, "%s: provider %s: skipped (model discovery unsupported)\n", binaryName, name)
			continue
		}
		result, err := service.ProviderModelCatalogContext(context.Background(), provider.ID, appsetup.ProviderSetupUpdate{})
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: provider %s: ERROR %s\n", binaryName, name, redactSecrets(err.Error(), providerSecret(cfg, provider.ID)))
			failures++
			continue
		}
		if result.Status != appsetup.ProviderModelStatusOK {
			_, _ = fmt.Fprintf(stdout, "%s: provider %s: %s %s\n", binaryName, name, strings.ToUpper(result.Status), redactSecrets(result.Message, providerSecret(cfg, provider.ID)))
			if provider.Configured && result.Status != appsetup.ProviderModelStatusRequiresKey && result.Status != appsetup.ProviderModelStatusUnsupported {
				failures++
			}
			if !provider.Configured && result.Status != appsetup.ProviderModelStatusRequiresKey && result.Status != appsetup.ProviderModelStatusUnsupported {
				failures++
			}
			continue
		}
		if metadata := providerCatalogMetadataSummary(result.Metadata); metadata != "" {
			_, _ = fmt.Fprintf(stdout, "%s: provider %s: ok (%d models, %s; %s)\n", binaryName, name, len(result.Models), result.Source, metadata)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s: provider %s: ok (%d models, %s)\n", binaryName, name, len(result.Models), result.Source)
	}
	if failures > 0 {
		_, _ = fmt.Fprintf(stdout, "%s: providers verify: failed (%d issue(s))\n", binaryName, failures)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s: providers verify: ok\n", binaryName)
	return 0
}

func providerCatalogMetadataSummary(metadata []providers.ModelMetadata) string {
	if len(metadata) == 0 {
		return ""
	}
	contextReady := 0
	toolReady := 0
	reasoningReady := 0
	liveReady := 0
	for _, item := range metadata {
		if item.ContextWindow > 0 {
			contextReady++
		}
		if item.ToolCalling {
			toolReady++
		}
		if item.ReasoningEffort {
			reasoningReady++
		}
		if item.Source == providers.ModelMetadataSourceLiveCatalog {
			liveReady++
		}
	}
	parts := []string{fmt.Sprintf("metadata context %d/%d", contextReady, len(metadata))}
	if toolReady > 0 {
		parts = append(parts, fmt.Sprintf("tools %d/%d", toolReady, len(metadata)))
	}
	if reasoningReady > 0 {
		parts = append(parts, fmt.Sprintf("reasoning %d/%d", reasoningReady, len(metadata)))
	}
	if liveReady > 0 {
		parts = append(parts, fmt.Sprintf("live %d/%d", liveReady, len(metadata)))
	}
	return strings.Join(parts, ", ")
}

func providerVerifyIncludesCatalogs(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--catalogs", "catalogs", "--all", "all":
			return true
		}
	}
	return false
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
