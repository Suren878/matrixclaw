package tools

const (
	namespaceCoreWeb  = "core.web"
	webFetchToolName  = "web_fetch"
	webSearchToolName = "web_search"

	defaultWebFetchMaxLength = 20000
	maxWebFetchMaxLength     = 100000
	defaultWebSearchLimit    = 8
	maxWebSearchLimit        = 20

	webFetchTimeout  = 15
	webSearchTimeout = 15
)

type WebFetchParams struct {
	URL       string `json:"url"`
	MaxLength int    `json:"max_length,omitempty"`
}

type WebFetchResponseMetadata struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	StatusCode  int    `json:"status_code,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	CharCount   int    `json:"char_count,omitempty"`
}

type WebSearchParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type WebSearchResult struct {
	Position    int    `json:"position"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type WebSearchResponseMetadata struct {
	Query    string            `json:"query"`
	Provider string            `json:"provider"`
	Results  []WebSearchResult `json:"results"`
}

type webFetchExecutor struct{}

type webSearchExecutor struct {
	config func() (WebSearchProviderConfig, error)
}

// WebSearchProviderConfig holds the active provider credentials for web search.
type WebSearchProviderConfig struct {
	Provider  string
	TavilyKey string
	SerperKey string
	BaseURL   string
}

func NewWebFetchExecutor() Executor { return &webFetchExecutor{} }

func NewWebSearchExecutor(config func() (WebSearchProviderConfig, error)) Executor {
	return &webSearchExecutor{config: config}
}

func (e *webFetchExecutor) Spec() Spec { return coreDefinitionSpec(webFetchToolName) }

func (e *webSearchExecutor) Spec() Spec {
	return Spec{
		ID:              webSearchToolName,
		Name:            "WebSearch",
		Description:     "Search the web and return titles, URLs, and descriptions",
		Risk:            RiskSafe,
		Effect:          EffectReadOnly,
		ApprovalMode:    ApprovalNever,
		Namespace:       namespaceCoreWeb,
		Category:        CategoryWeb,
		Profiles:        []Profile{ProfileCoding, ProfileWeb},
		OutputKind:      OutputSearchResults,
		InputJSONSchema: webSearchInputSchema,
	}
}
