package providers

type ProviderPolicy struct {
	RequestedID         string
	CatalogID           string
	Known               bool
	Implemented         bool
	Name                string
	Type                string
	AuthMode            ProviderAuthMode
	RequiresAPIKey      bool
	AuthStatusLabel     string
	Transport           ProviderTransport
	RuntimeProviderType string
	RequiresBaseURL     bool
	Capabilities        Capabilities
	ModelsURL           string
	PublicModelCatalog  bool
	SupportsHealthCheck bool
	OpenAIChat          OpenAIChatOptions
	DefaultBaseURL      string
	BaseURLOptions      []BaseURLOption
	DefaultModel        string
	APIKeyEnv           string
	Notes               string
}

func PolicyForProvider(providerID string, providerType string) ProviderPolicy {
	requestedID := NormalizeProviderID(providerID)
	if spec, ok := ProviderSpecByID(requestedID); ok {
		entry := spec.Entry
		authMode := spec.Auth
		return ProviderPolicy{
			RequestedID:         requestedID,
			CatalogID:           entry.ID,
			Known:               true,
			Implemented:         entry.Implemented,
			Name:                entry.Name,
			Type:                entry.Type,
			AuthMode:            authMode,
			RequiresAPIKey:      authModeRequiresAPIKey(authMode),
			AuthStatusLabel:     authModeStatusLabel(authMode),
			Transport:           spec.Transport,
			RuntimeProviderType: spec.RuntimeProviderType,
			RequiresBaseURL:     entry.RequiresBaseURL,
			Capabilities:        entry.Capabilities,
			ModelsURL:           spec.ModelsURL,
			PublicModelCatalog:  spec.PublicModelCatalog,
			SupportsHealthCheck: entry.Capabilities.ModelDiscovery && !spec.DisableHealthCheck,
			OpenAIChat:          cloneOpenAIChatOptions(spec.OpenAIChat),
			DefaultBaseURL:      entry.DefaultBaseURL,
			BaseURLOptions:      append([]BaseURLOption(nil), entry.BaseURLOptions...),
			DefaultModel:        entry.DefaultModel,
			APIKeyEnv:           entry.APIKeyEnv,
			Notes:               entry.Notes,
		}
	}

	normalizedType := NormalizeOptionalProviderType(providerType)
	authMode := defaultProviderAuthMode(normalizedType)
	transport := defaultProviderTransport(normalizedType)
	return ProviderPolicy{
		RequestedID:         requestedID,
		CatalogID:           requestedID,
		Type:                normalizedType,
		AuthMode:            authMode,
		RequiresAPIKey:      authModeRequiresAPIKey(authMode),
		AuthStatusLabel:     authModeStatusLabel(authMode),
		Transport:           transport,
		RuntimeProviderType: RuntimeProviderTypeForTransport(transport, normalizedType),
		RequiresBaseURL:     normalizedType != "",
		Capabilities:        defaultCapabilitiesForCustomProviderType(normalizedType),
		SupportsHealthCheck: normalizedType != "",
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

func authModeRequiresAPIKey(authMode ProviderAuthMode) bool {
	switch authMode {
	case ProviderAuthOAuth, ProviderAuthExternal:
		return false
	default:
		return true
	}
}

func authModeStatusLabel(authMode ProviderAuthMode) string {
	switch authMode {
	case ProviderAuthOAuth:
		return "OAuth"
	case ProviderAuthExternal:
		return "External"
	default:
		return ""
	}
}
