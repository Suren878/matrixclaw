package setup

import "strings"

func ProviderModelCatalogMessage(response ProviderModelsResponse) string {
	if message := strings.TrimSpace(response.Message); message != "" {
		return message
	}
	switch response.Status {
	case ProviderModelStatusRequiresKey:
		return "API key required"
	case ProviderModelStatusUnsupported:
		return "Model discovery is not supported."
	case ProviderModelStatusAuthError:
		return "Could not verify API key or load models."
	default:
		return "Could not load remote models."
	}
}

func ProviderModelCatalogAllowsManualInput(response ProviderModelsResponse) bool {
	return response.ManualInput
}

func ProviderModelCatalogManualMessage(response ProviderModelsResponse) string {
	message := strings.TrimSpace(ProviderModelCatalogMessage(response))
	if message == "" {
		return "Enter the model manually."
	}
	return strings.TrimRight(message, ".!?") + ". Enter the model manually."
}
