package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func registerLocalContextWindows(ctx context.Context, cfg Config, client *http.Client, baseURL string, apiKey string, models []string) {
	if !shouldProbeLocalContext(cfg, baseURL) {
		return
	}
	serverURL := strings.TrimRight(baseURL, "/")
	serverURL = strings.TrimSuffix(serverURL, "/v1")
	if serverURL == "" {
		return
	}
	headers := func(req *http.Request) {
		req.Header.Set("Accept", "application/json")
		if strings.TrimSpace(apiKey) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
		}
	}
	registerLMStudioContextWindows(ctx, cfg, client, serverURL, headers)
	for _, model := range models {
		registerOllamaContextWindow(ctx, cfg, client, serverURL, headers, model)
		registerOpenAIModelContextWindow(ctx, cfg, client, serverURL, headers, model)
	}
}

func shouldProbeLocalContext(cfg Config, baseURL string) bool {
	hint := strings.ToLower(strings.TrimSpace(cfg.ProviderID + " " + cfg.CatalogID + " " + baseURL))
	if strings.Contains(hint, "ollama") || strings.Contains(hint, "lm-studio") || strings.Contains(hint, "lmstudio") || strings.Contains(hint, "llama.cpp") || strings.Contains(hint, "local") {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "::1"
}

func registerOllamaContextWindow(ctx context.Context, cfg Config, client *http.Client, serverURL string, setHeaders func(*http.Request), model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	body, err := json.Marshal(map[string]string{"name": model})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return
	}
	setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return
	}
	var payload struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return
	}
	tokens := parseOllamaNumCtx(payload.Parameters)
	if tokens == 0 {
		tokens = contextWindowFromMap(payload.ModelInfo)
	}
	registerDiscoveredContextWindow(cfg, model, tokens)
}

func registerLMStudioContextWindows(ctx context.Context, cfg Config, client *http.Client, serverURL string, setHeaders func(*http.Request)) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/v1/models", nil)
	if err != nil {
		return
	}
	setHeaders(req)
	res, err := client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return
	}
	var payload struct {
		Models []struct {
			ID               string `json:"id"`
			Key              string `json:"key"`
			MaxContextLength int    `json:"max_context_length"`
			LoadedInstances  []struct {
				Config struct {
					ContextLength int `json:"context_length"`
				} `json:"config"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return
	}
	for _, model := range payload.Models {
		id := firstNonEmptyString(model.ID, model.Key)
		tokens := model.MaxContextLength
		for _, instance := range model.LoadedInstances {
			if instance.Config.ContextLength > 0 {
				tokens = instance.Config.ContextLength
				break
			}
		}
		registerDiscoveredContextWindow(cfg, id, tokens)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func registerOpenAIModelContextWindow(ctx context.Context, cfg Config, client *http.Client, serverURL string, setHeaders func(*http.Request), model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/v1/models/"+url.PathEscape(model), nil)
	if err != nil {
		return
	}
	setHeaders(req)
	res, err := client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return
	}
	registerDiscoveredContextWindow(cfg, model, contextWindowFromMap(payload))
}

func registerDiscoveredContextWindow(cfg Config, model string, tokens int) {
	if tokens <= 0 {
		return
	}
	providers.RegisterContextWindowTokens(cfg.ProviderID, providers.TypeOpenAICompat, model, tokens)
	providers.RegisterContextWindowTokens(cfg.CatalogID, providers.TypeOpenAICompat, model, tokens)
}

func parseOllamaNumCtx(parameters string) int {
	for _, line := range strings.Split(parameters, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "num_ctx" || i+1 >= len(fields) {
				continue
			}
			value, err := strconv.Atoi(fields[i+1])
			if err == nil && value > 0 {
				return value
			}
		}
	}
	return 0
}

func contextWindowFromMap(values map[string]any) int {
	for _, key := range []string{
		"context_length",
		"context_window",
		"context_size",
		"max_context_length",
		"max_position_embeddings",
		"max_model_len",
		"max_input_tokens",
		"max_sequence_length",
		"max_seq_len",
		"n_ctx_train",
		"n_ctx",
		"ctx_size",
		"max_tokens",
	} {
		if tokens := numericMapValue(values, key); tokens > 0 {
			return tokens
		}
	}
	return 0
}

func numericMapValue(values map[string]any, key string) int {
	value, ok := values[key]
	if !ok {
		return 0
	}
	switch value := value.(type) {
	case float64:
		if value > 0 {
			return int(value)
		}
	case int:
		if value > 0 {
			return value
		}
	case json.Number:
		parsed, err := value.Int64()
		if err == nil && parsed > 0 {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}
