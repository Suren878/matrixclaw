package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const (
	defaultTimeout          = 90 * time.Second
	defaultAnthropicVersion = "2023-06-01"
	defaultMaxTokens        = 4096
)

type Config struct {
	ProviderID      string
	CatalogID       string
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	Profile         providers.ProviderProfile
	HTTPClient      *http.Client
}

type Runtime struct {
	client       *http.Client
	endpoint     string
	apiKey       string
	model        string
	maxTokens    int64
	profile      providers.RuntimeProfile
	capabilities providers.ModelCapabilities
}

func New(_ context.Context, cfg Config) (providers.Runtime, error) {
	client, apiKey, baseURL, model, maxTokens, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	providerProfile := cfg.Profile
	if providerProfile == (providers.ProviderProfile{}) {
		providerProfile = providers.ProfileForProvider(providers.TypeAnthropic)
	}

	return &Runtime{
		client:       client,
		endpoint:     strings.TrimRight(baseURL, "/") + "/messages",
		apiKey:       apiKey,
		model:        model,
		maxTokens:    maxTokens,
		profile:      providerProfile.RuntimeProfile,
		capabilities: providerProfile.Capabilities,
	}, nil
}

func (r *Runtime) RuntimeProfile() providers.RuntimeProfile {
	return r.profile
}

func (r *Runtime) ModelCapabilities() providers.ModelCapabilities {
	return r.capabilities
}

func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	client, apiKey, baseURL, _, _, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build models request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", defaultAnthropicVersion)
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: models request failed: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read models response: %w", err)
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("anthropic: model listing unavailable: %s", decodeAnthropicError(res.StatusCode, body))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic: %s", decodeAnthropicError(res.StatusCode, body))
	}

	var payload struct {
		Data []struct {
			ID             string `json:"id"`
			MaxInputTokens int    `json:"max_input_tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("anthropic: decode models response: %w", err)
	}

	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			providers.RegisterContextWindowTokens(cfg.ProviderID, providers.TypeAnthropic, id, item.MaxInputTokens)
			providers.RegisterContextWindowTokens(cfg.CatalogID, providers.TypeAnthropic, id, item.MaxInputTokens)
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("anthropic: no models available")
	}
	return models, nil
}

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	if unsupported := unsupportedToolUse(request); unsupported != "" {
		return providers.Response{}, fmt.Errorf("anthropic: tool use disabled by runtime profile; unsupported %s present", unsupported)
	}

	request = providers.NormalizeRequest(request, r.profile)
	payload := anthropicRequest{
		Model:     r.model,
		MaxTokens: r.maxTokens,
		System:    combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions),
		Messages:  make([]anthropicMessage, 0, len(request.Messages)),
	}

	for _, message := range request.Messages {
		if content := strings.TrimSpace(message.Content); content != "" {
			payload.Messages = append(payload.Messages, anthropicMessage{
				Role:    normalizeAnthropicRole(message.Role),
				Content: content,
			})
		}
	}

	if len(payload.Messages) == 0 {
		return providers.Response{}, errors.New("anthropic: no messages")
	}
	if providers.TextStreamFromContext(ctx) != nil {
		payload.Stream = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("x-api-key", r.apiKey)
	httpReq.Header.Set("anthropic-version", defaultAnthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if payload.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	httpRes, err := r.client.Do(httpReq)
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer httpRes.Body.Close()

	if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
		resBody, err := io.ReadAll(httpRes.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("anthropic: read response: %w", err)
		}
		return providers.Response{}, fmt.Errorf("anthropic: %s", decodeAnthropicError(httpRes.StatusCode, resBody))
	}
	if payload.Stream {
		return r.decodeStream(ctx, httpRes.Body)
	}

	resBody, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: read response: %w", err)
	}

	var response anthropicResponse
	if err := json.Unmarshal(resBody, &response); err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	var text strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			text.WriteString(block.Text)
		}
	}

	reply := strings.TrimSpace(text.String())
	if reply == "" {
		return providers.Response{}, errors.New("anthropic: empty assistant reply")
	}

	return providers.Response{
		Text:     reply,
		Model:    r.model,
		Provider: providers.TypeAnthropic,
		Usage:    anthropicUsage(response.Usage),
	}, nil
}

func unsupportedToolUse(request providers.Request) string {
	if len(request.Tools) > 0 {
		return "tool definitions"
	}
	for _, message := range request.Messages {
		if len(message.Images) > 0 {
			return "image inputs"
		}
		if len(message.ToolCalls) > 0 {
			return "assistant tool-call messages"
		}
		if strings.TrimSpace(message.Role) == "tool" || strings.TrimSpace(message.ToolCallID) != "" {
			return "tool-result messages"
		}
	}
	return ""
}

func combinedSystemPrompt(systemPrompt string, customInstructions string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	customInstructions = strings.TrimSpace(customInstructions)
	if customInstructions == "" {
		return systemPrompt
	}
	block := "User custom instructions:\n" + customInstructions
	if systemPrompt == "" {
		return block
	}
	return systemPrompt + "\n\n" + block
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int64              `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	Usage anthropicUsagePayload `json:"usage,omitempty"`
}

type anthropicUsagePayload struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

func anthropicUsage(usage anthropicUsagePayload) providers.Usage {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 &&
		usage.CacheCreationInputTokens == 0 && usage.CacheReadInputTokens == 0 {
		return providers.Usage{}
	}
	raw, _ := json.Marshal(usage)
	return providers.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.InputTokens + usage.OutputTokens,
		CachedTokens: usage.CacheCreationInputTokens + usage.CacheReadInputTokens,
		ProviderRaw:  raw,
	}
}

type anthropicStreamDelta struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type anthropicErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func decodeAnthropicError(statusCode int, body []byte) string {
	var envelope anthropicErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return fmt.Sprintf("status %d: %s", statusCode, strings.TrimSpace(envelope.Error.Message))
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Sprintf("status %d", statusCode)
	}
	return fmt.Sprintf("status %d: %s", statusCode, text)
}

func normalizeConfig(cfg Config) (*http.Client, string, string, string, int64, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, "", "", "", 0, errors.New("anthropic: api key is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, "", "", "", 0, errors.New("anthropic: base url is required")
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = providers.DefaultAnthropicModel
	}

	maxTokens := cfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}

	return client, apiKey, baseURL, model, maxTokens, nil
}

func normalizeAnthropicRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "assistant"
	default:
		return "user"
	}
}

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var text strings.Builder
	if err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "" {
			return nil
		}

		var chunk anthropicStreamDelta
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("anthropic: decode stream chunk: %w", err)
		}
		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return errors.New(strings.TrimSpace(chunk.Error.Message))
		}

		delta := ""
		switch event.Type {
		case "content_block_delta":
			delta = chunk.Delta.Text
		case "content_block_start":
			delta = chunk.ContentBlock.Text
		}
		if delta == "" {
			return nil
		}

		text.WriteString(delta)
		return providers.StreamText(ctx, delta)
	}); err != nil {
		return providers.Response{}, err
	}

	reply := strings.TrimSpace(text.String())
	if reply == "" {
		return providers.Response{}, errors.New("anthropic: empty assistant reply")
	}
	return providers.Response{
		Text:     reply,
		Model:    r.model,
		Provider: providers.TypeAnthropic,
	}, nil
}
