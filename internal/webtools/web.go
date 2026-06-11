package webtools

import (
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	namespaceCoreWeb          = "core.web"
	webFetchToolName          = "web_fetch"
	webSearchToolName         = "web_search"
	webResearchToolName       = "web_research"
	webResearchAskToolName    = "web_research_ask"
	webResearchStatusToolName = "web_research_status"

	defaultWebFetchMaxLength = 20000
	maxWebFetchMaxLength     = 100000
	defaultWebSearchLimit    = 8
	maxWebSearchLimit        = 20

	webFetchTimeout  = 15
	webSearchTimeout = 15
)

type WebFetchParams struct {
	URL       string `json:"url"`
	Task      string `json:"task,omitempty"`
	MaxLength int    `json:"max_length,omitempty"`
}

type WebFetchResponseMetadata struct {
	URL         string   `json:"url"`
	Title       string   `json:"title,omitempty"`
	StatusCode  int      `json:"status_code,omitempty"`
	ContentType string   `json:"content_type,omitempty"`
	Truncated   bool     `json:"truncated,omitempty"`
	CharCount   int      `json:"char_count,omitempty"`
	ResearchID  string   `json:"research_id,omitempty"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
}

type WebFetchedPage struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Text        string `json:"text,omitempty"`
	HTML        string `json:"html,omitempty"`
	StatusCode  int    `json:"status_code,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
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

type webFetchExecutor struct {
	web *WebService
}

type webSearchExecutor struct {
	web *WebService
}

// WebSearchProviderConfig holds the active provider credentials for web search.
type WebSearchProviderConfig struct {
	Provider  string
	TavilyKey string
	SerperKey string
	BaseURL   string
}

func NewWebFetchExecutor() tools.Executor {
	return NewWebFetchExecutorWithService(nil)
}

func NewWebFetchExecutorWithService(web *WebService) tools.Executor {
	return &webFetchExecutor{web: web}
}

func NewWebSearchExecutor(config func() (WebSearchProviderConfig, error)) tools.Executor {
	return NewWebSearchExecutorWithService(NewWebService(config, nil))
}

func NewWebSearchExecutorWithService(web *WebService) tools.Executor {
	return &webSearchExecutor{web: web}
}

func (e *webFetchExecutor) Spec() tools.Spec {
	return tools.Spec{
		ID:              webFetchToolName,
		Name:            "WebFetch",
		Description:     "Fetch a URL through compact web research artifacts; task mode returns extracted facts/result",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       namespaceCoreWeb,
		Category:        tools.CategoryWeb,
		Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileWeb},
		OutputKind:      tools.OutputWebContent,
		InputJSONSchema: webFetchInputSchema,
	}
}

func (e *webSearchExecutor) Spec() tools.Spec {
	return tools.Spec{
		ID:              webSearchToolName,
		Name:            "WebSearch",
		Description:     "Search the web and return titles, URLs, and descriptions",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       namespaceCoreWeb,
		Category:        tools.CategoryWeb,
		Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileWeb},
		OutputKind:      tools.OutputSearchResults,
		InputJSONSchema: webSearchInputSchema,
	}
}
