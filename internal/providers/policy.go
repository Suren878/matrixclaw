package providers

// ProviderPolicy is the catalog entry of a built-in provider, or the type
// defaults of a custom one. CatalogID is the canonical id an alias stands for.
type ProviderPolicy struct {
	CatalogID          string
	Known              bool
	Name               string
	Type               string
	AuthMode           ProviderAuthMode
	RequiresAPIKey     bool
	AuthStatusLabel    string
	RequiresBaseURL    bool
	Capabilities       Capabilities
	PublicModelCatalog bool
	ChatHeaders        map[string]string
	DefaultBaseURL     string
	BaseURLOptions     []BaseURLOption
	DefaultModel       string
	APIKeyEnv          string
	Notes              string
}

func PolicyForProvider(providerID string, providerType string) ProviderPolicy {
	requestedID := NormalizeProviderID(providerID)
	if spec, ok := ProviderSpecByID(requestedID); ok {
		entry := spec.Entry
		authMode := spec.Auth
		return ProviderPolicy{
			CatalogID:          entry.ID,
			Known:              true,
			Name:               entry.Name,
			Type:               entry.Type,
			AuthMode:           authMode,
			RequiresAPIKey:     authMode == ProviderAuthAPIKey,
			AuthStatusLabel:    authModeStatusLabel(authMode),
			RequiresBaseURL:    entry.RequiresBaseURL,
			Capabilities:       entry.Capabilities,
			PublicModelCatalog: spec.PublicModelCatalog,
			ChatHeaders:        copyStringMap(spec.ChatHeaders),
			DefaultBaseURL:     entry.DefaultBaseURL,
			BaseURLOptions:     append([]BaseURLOption(nil), entry.BaseURLOptions...),
			DefaultModel:       entry.DefaultModel,
			APIKeyEnv:          entry.APIKeyEnv,
			Notes:              entry.Notes,
		}
	}

	normalizedType := NormalizeOptionalProviderType(providerType)
	authMode := defaultProviderAuthMode(normalizedType)
	return ProviderPolicy{
		CatalogID:       requestedID,
		Type:            normalizedType,
		AuthMode:        authMode,
		RequiresAPIKey:  authMode == ProviderAuthAPIKey,
		AuthStatusLabel: authModeStatusLabel(authMode),
		RequiresBaseURL: normalizedType != "",
		Capabilities:    defaultCapabilitiesForCustomProviderType(normalizedType),
	}
}

func CanonicalProviderID(providerID string) string {
	policy := PolicyForProvider(providerID, "")
	if policy.Known {
		return policy.CatalogID
	}
	return NormalizeProviderID(providerID)
}

func defaultProviderAuthMode(providerType string) ProviderAuthMode {
	if NormalizeProviderType(providerType) == TypeOpenAICodex {
		return ProviderAuthOAuth
	}
	return ProviderAuthAPIKey
}

func authModeStatusLabel(authMode ProviderAuthMode) string {
	if authMode == ProviderAuthOAuth {
		return "OAuth"
	}
	return ""
}
