package providers

import (
	"net/url"
	"strings"
)

// OpenAIChatOptions are what an OpenAI-compatible endpoint needs beyond the
// standard chat completions request.
type OpenAIChatOptions struct {
	Headers             map[string]string
	MaxCompletionTokens bool // send max_completion_tokens instead of max_tokens
	PromptCacheKey      bool // send prompt_cache_key; only OpenAI's own endpoint is known to accept it
	ContentCacheControl bool // mark cache breakpoints in content parts; OpenRouter passes them to Claude models
}

func ResolveOpenAIChatOptions(profile ProviderProfile, baseURL string, model string) OpenAIChatOptions {
	headers := copyStringMap(profile.ChatHeaders)
	if headers == nil {
		headers = map[string]string{}
	}
	if headerValue(headers, "User-Agent") == "" {
		headers["User-Agent"] = "matrixclaw"
	}
	openAIHost := openAICompatibleHost(baseURL, "api.openai.com")
	return OpenAIChatOptions{
		Headers:             headers,
		MaxCompletionTokens: NormalizeProviderID(profile.ProviderID) == "openai" || openAIHost && strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5"),
		PromptCacheKey:      openAIHost,
		ContentCacheControl: openAICompatibleHost(baseURL, "openrouter.ai") && claudeModel(model),
	}
}

// claudeModel reports whether a gateway model ID names an Anthropic Claude model.
func claudeModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "anthropic/") || strings.Contains(model, "claude")
}

func openAICompatibleHost(rawURL string, host string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(parsed.Host))
	return normalized == host || normalized == host+":443"
}

func headerValue(headers map[string]string, key string) string {
	for name, value := range headers {
		if strings.EqualFold(strings.TrimSpace(name), key) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}
