package webtools

import (
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	namespaceCoreWeb  = "core.web"
	webFetchToolName  = "web_fetch"
	webSearchToolName = "web_search"

	defaultWebSearchLimit = 8
	maxWebSearchLimit     = 20

	webFetchTimeout  = 15
	webSearchTimeout = 15
)

type WebFetchParams struct {
	URL string `json:"url"`
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

// SearchConfig holds the active provider credentials for web search.
type SearchConfig struct {
	Provider  string
	TavilyKey string
	SerperKey string
	BaseURL   string
}

type webFetchExecutor struct{}

type webSearchExecutor struct {
	config func() (SearchConfig, error)
}

// NewFetchTool is web_fetch: one public page as readable text.
func NewFetchTool() tools.Executor {
	return webFetchExecutor{}
}

// NewSearchTool is web_search over the provider config reads at each call;
// nil config searches DuckDuckGo.
func NewSearchTool(config func() (SearchConfig, error)) tools.Executor {
	return webSearchExecutor{config: config}
}

func (webFetchExecutor) Spec() tools.Spec {
	return tools.Spec{
		ID:              webFetchToolName,
		Description:     "Fetch a public http(s) URL and return its main content as markdown",
		Effect:          tools.EffectReadOnly,
		Namespace:       namespaceCoreWeb,
		Category:        tools.CategoryWeb,
		InputJSONSchema: webFetchInputSchema,
	}
}

func (webSearchExecutor) Spec() tools.Spec {
	return tools.Spec{
		ID:              webSearchToolName,
		Description:     "Search the web and return titles, URLs, and descriptions",
		Effect:          tools.EffectReadOnly,
		Namespace:       namespaceCoreWeb,
		Category:        tools.CategoryWeb,
		InputJSONSchema: webSearchInputSchema,
	}
}
