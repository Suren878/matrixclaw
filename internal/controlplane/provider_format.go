package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func findSetupProvider(items []setup.ProviderSetupItem, providerID string) (setup.ProviderSetupItem, bool) {
	providerID = strings.TrimSpace(providerID)
	canonicalID := providers.CanonicalProviderID(providerID)
	for _, provider := range items {
		for _, id := range []string{provider.ID, provider.CatalogID} {
			if providers.CanonicalProviderID(id) == canonicalID || strings.EqualFold(strings.TrimSpace(id), providerID) {
				return provider, true
			}
		}
	}
	return setup.ProviderSetupItem{}, false
}

func providerListInfo(provider setup.ProviderSetupItem) string {
	return setup.ProviderCompactStatus(provider)
}

func providerKeyPrompt(provider setup.ProviderSetupItem) Result {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = strings.TrimSpace(provider.ID)
	}
	return Result{
		Handled: true,
		Prompt: &PromptData{
			Title:               "API key for " + name,
			Placeholder:         "Paste API key",
			SubmitCommandPrefix: providerKeyCommandPrefix(provider.ID),
			CancelCommand:       providerCommand(),
			Sensitive:           true,
		},
	}
}
