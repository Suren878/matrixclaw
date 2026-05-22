package providers

import "sort"

type ProviderAuthMode string
type ProviderTransport string

const (
	ProviderAuthAPIKey   ProviderAuthMode = "api_key"
	ProviderAuthOAuth    ProviderAuthMode = "oauth"
	ProviderAuthExternal ProviderAuthMode = "external"
)

const (
	ProviderTransportOpenAIChat       ProviderTransport = "openai_chat"
	ProviderTransportOpenAIResponses  ProviderTransport = "openai_responses"
	ProviderTransportAnthropicMessage ProviderTransport = "anthropic_messages"
	ProviderTransportGeminiNative     ProviderTransport = "gemini_native"
)

type ProviderSpec struct {
	Order               int
	Entry               CatalogEntry
	Aliases             []string
	Auth                ProviderAuthMode
	Transport           ProviderTransport
	RuntimeProviderType string
	ModelsURL           string
	PublicModelCatalog  bool
	DisableHealthCheck  bool
	OpenAIChat          OpenAIChatOptions
}

var providerSpecs []ProviderSpec

func registerProvider(spec ProviderSpec) {
	spec.Entry.ID = NormalizeProviderID(spec.Entry.ID)
	spec.Entry.CatalogID = NormalizeProviderID(spec.Entry.CatalogID)
	spec.Entry.Type = NormalizeProviderType(spec.Entry.Type)
	if spec.Entry.CatalogID == "" {
		spec.Entry.CatalogID = spec.Entry.ID
	}
	if spec.Entry.ID == "" || spec.Entry.Name == "" || spec.Entry.Type == "" {
		panic("providers: provider spec requires id, name, and type")
	}
	if spec.Auth == "" {
		spec.Auth = ProviderAuthAPIKey
	}
	if spec.Transport == "" {
		spec.Transport = defaultProviderTransport(spec.Entry.Type)
	}
	if spec.Transport == ProviderTransportOpenAIResponses && spec.RuntimeProviderType == "" {
		panic("providers: openai_responses transport requires explicit runtime provider type")
	}
	if spec.RuntimeProviderType == "" {
		spec.RuntimeProviderType = RuntimeProviderTypeForTransport(spec.Transport, spec.Entry.Type)
	}
	ids := append([]string{spec.Entry.ID}, spec.Aliases...)
	for _, existing := range providerSpecs {
		existingIDs := append([]string{existing.Entry.ID}, existing.Aliases...)
		for _, id := range ids {
			normalizedID := NormalizeProviderID(id)
			if normalizedID == "" {
				continue
			}
			for _, existingID := range existingIDs {
				if normalizedID == NormalizeProviderID(existingID) {
					panic("providers: duplicate provider id or alias " + normalizedID)
				}
			}
		}
	}
	providerSpecs = append(providerSpecs, spec)
}

func ProviderSpecs() []ProviderSpec {
	out := make([]ProviderSpec, len(providerSpecs))
	copy(out, providerSpecs)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order == out[j].Order {
			return out[i].Entry.ID < out[j].Entry.ID
		}
		return out[i].Order < out[j].Order
	})
	return out
}

func ProviderSpecByID(providerID string) (ProviderSpec, bool) {
	providerID = NormalizeProviderID(providerID)
	if providerID == "" {
		return ProviderSpec{}, false
	}
	for _, spec := range ProviderSpecs() {
		if spec.Entry.ID == providerID {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if NormalizeProviderID(alias) == providerID {
				return spec, true
			}
		}
	}
	return ProviderSpec{}, false
}

func ProviderAuthModeFor(providerID string, providerType string) ProviderAuthMode {
	return PolicyForProvider(providerID, providerType).AuthMode
}

func ProviderRequiresAPIKey(providerID string, providerType string) bool {
	return PolicyForProvider(providerID, providerType).RequiresAPIKey
}

func ProviderTransportFor(providerID string, providerType string) ProviderTransport {
	return PolicyForProvider(providerID, providerType).Transport
}

func RuntimeProviderTypeFor(providerID string, providerType string) string {
	return PolicyForProvider(providerID, providerType).RuntimeProviderType
}

func RuntimeProviderTypeForTransport(transport ProviderTransport, providerType string) string {
	switch transport {
	case ProviderTransportAnthropicMessage:
		return TypeAnthropic
	case ProviderTransportGeminiNative:
		return TypeGemini
	default:
		return NormalizeProviderType(providerType)
	}
}

func defaultProviderTransport(providerType string) ProviderTransport {
	switch NormalizeProviderType(providerType) {
	case TypeOpenAICodex:
		return ProviderTransportOpenAIResponses
	case TypeAnthropic:
		return ProviderTransportAnthropicMessage
	case TypeGemini:
		return ProviderTransportGeminiNative
	default:
		return ProviderTransportOpenAIChat
	}
}
