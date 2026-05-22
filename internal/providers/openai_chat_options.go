package providers

import (
	"net/url"
	"strings"
)

type OpenAIChatMaxTokensField string

const (
	OpenAIChatMaxTokensAuto       OpenAIChatMaxTokensField = ""
	OpenAIChatMaxTokens           OpenAIChatMaxTokensField = "max_tokens"
	OpenAIChatMaxCompletionTokens OpenAIChatMaxTokensField = "max_completion_tokens"
)

type OpenAIChatOptions struct {
	DefaultHeaders map[string]string
	MaxTokensField OpenAIChatMaxTokensField
	RequestQuirks  OpenAIChatRequestQuirks
}

type OpenAIChatRequestQuirks struct {
	RetryUnsupportedReasoningEffort bool
	RetryMaxTokensField             bool
	RetryAssistantReasoningContent  bool
}

func ResolveOpenAIChatOptions(profile ProviderProfile, baseURL string, model string) OpenAIChatOptions {
	options := cloneOpenAIChatOptions(profile.OpenAIChat)
	if options.DefaultHeaders == nil {
		options.DefaultHeaders = map[string]string{}
	}
	if headerValue(options.DefaultHeaders, "User-Agent") == "" {
		options.DefaultHeaders["User-Agent"] = "matrixclaw"
	}
	if options.MaxTokensField == OpenAIChatMaxTokensAuto {
		options.MaxTokensField = resolveOpenAIChatMaxTokensField(profile, baseURL, model)
	}
	options.RequestQuirks = normalizeOpenAIChatRequestQuirks(options.RequestQuirks)
	return options
}

func cloneOpenAIChatOptions(options OpenAIChatOptions) OpenAIChatOptions {
	return OpenAIChatOptions{
		DefaultHeaders: copyStringMap(options.DefaultHeaders),
		MaxTokensField: normalizeOpenAIChatMaxTokensField(options.MaxTokensField),
		RequestQuirks:  normalizeOpenAIChatRequestQuirks(options.RequestQuirks),
	}
}

func normalizeOpenAIChatRequestQuirks(quirks OpenAIChatRequestQuirks) OpenAIChatRequestQuirks {
	return OpenAIChatRequestQuirks{
		RetryUnsupportedReasoningEffort: true,
		RetryMaxTokensField:             true,
		RetryAssistantReasoningContent:  true,
	}
}

func normalizeOpenAIChatMaxTokensField(value OpenAIChatMaxTokensField) OpenAIChatMaxTokensField {
	switch OpenAIChatMaxTokensField(strings.ToLower(strings.TrimSpace(string(value)))) {
	case OpenAIChatMaxCompletionTokens:
		return OpenAIChatMaxCompletionTokens
	case OpenAIChatMaxTokens:
		return OpenAIChatMaxTokens
	default:
		return OpenAIChatMaxTokensAuto
	}
}

func resolveOpenAIChatMaxTokensField(profile ProviderProfile, baseURL string, model string) OpenAIChatMaxTokensField {
	providerID := NormalizeProviderID(profile.ProviderID)
	if providerID == "openai" {
		return OpenAIChatMaxCompletionTokens
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5") && openAICompatibleHost(baseURL, "api.openai.com") {
		return OpenAIChatMaxCompletionTokens
	}
	return OpenAIChatMaxTokens
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
