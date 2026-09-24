package openaicodex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type Config struct {
	ProviderID      string
	CatalogID       string
	BaseURL         string
	Model           string
	ReasoningEffort string
	ToolUseMode     providers.ToolUseMode
	Profile         providers.ProviderProfile
	HTTPClient      *http.Client
}

type Runtime struct {
	client          *http.Client
	baseURL         string
	model           string
	reasoningEffort string
	profile         providers.RuntimeProfile
	capabilities    providers.ModelCapabilities
}

func New(_ context.Context, cfg Config) (providers.Runtime, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = providers.NewHTTPClient()
	}
	baseURL := strings.TrimRight(firstNonEmpty(cfg.BaseURL, DefaultBaseURL), "/")
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = providers.DefaultOpenAICodexModel
	}
	providerProfile := cfg.Profile
	if providerProfile.IsZero() {
		providerProfile = providers.ProfileForModel("openai-codex", providers.TypeOpenAICodex, model)
	}
	profile := providerProfile.RuntimeProfileWithOverrides(providers.RuntimeProfile{
		ToolUseMode: cfg.ToolUseMode,
	})
	reasoningEffort := ""
	if providerProfile.SupportsReasoningEffort {
		reasoningEffort = providers.NormalizeReasoningEffort(cfg.ReasoningEffort)
		if reasoningEffort == providers.ReasoningEffortNone {
			reasoningEffort = ""
		}
	}
	return &Runtime{
		client:          client,
		baseURL:         baseURL,
		model:           model,
		reasoningEffort: reasoningEffort,
		profile:         profile,
		capabilities:    providerProfile.Capabilities,
	}, nil
}

func (r *Runtime) RuntimeProfile() providers.RuntimeProfile {
	return r.profile
}

func (r *Runtime) ModelCapabilities() providers.ModelCapabilities {
	return r.capabilities
}

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	request = providers.NormalizeRequest(request, r.profile)
	payload := r.responsesPayload(request)
	if len(payload.Input) == 0 {
		return providers.Response{}, errors.New("openai-codex: no input")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: marshal request: %w", err)
	}
	creds, err := ResolveCredentials(ctx, r.client, r.baseURL)
	if err != nil {
		return providers.Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(creds.BaseURL, "/")+"/responses?client_version="+codexClientVersion, bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: build request: %w", err)
	}
	setCodexHeaders(req, creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := r.client.Do(req)
	if err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openai-codex: read response: %w", err)
		}
		return providers.Response{}, fmt.Errorf("openai-codex: %s", decodeError(raw))
	}
	if strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "application/json") {
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openai-codex: read response: %w", err)
		}
		return r.decodeResponse(raw)
	}
	return r.decodeStream(ctx, res.Body)
}

type responsesRequest struct {
	Model             string              `json:"model"`
	Instructions      string              `json:"instructions,omitempty"`
	Input             []responsesItem     `json:"input"`
	Tools             []responsesTool     `json:"tools,omitempty"`
	ToolChoice        string              `json:"tool_choice,omitempty"`
	Reasoning         *responsesReasoning `json:"reasoning,omitempty"`
	ParallelToolCalls bool                `json:"parallel_tool_calls,omitempty"`
	Store             bool                `json:"store"`
	Include           []string            `json:"include,omitempty"`
	PromptCacheKey    string              `json:"prompt_cache_key,omitempty"`
	Stream            bool                `json:"stream"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesItem struct {
	Type      string                 `json:"type,omitempty"`
	Role      string                 `json:"role,omitempty"`
	Content   []responsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
	Output    string                 `json:"output,omitempty"`
	// A replayed reasoning item must carry summary, even when it is empty.
	Summary          *[]responsesSummaryPart `json:"summary,omitempty"`
	EncryptedContent string                  `json:"encrypted_content,omitempty"`
}

type responsesSummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Refusal  string `json:"refusal,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict"`
}

type responsesResponse struct {
	Status            string          `json:"status,omitempty"`
	Error             *responsesError `json:"error,omitempty"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details,omitempty"`
	Output []responsesOutputItem `json:"output"`
	Usage  responsesUsage        `json:"usage,omitempty"`
}

type responsesStreamEvent struct {
	Type     string            `json:"type,omitempty"`
	Delta    string            `json:"delta,omitempty"`
	Response responsesResponse `json:"response,omitempty"`
	Error    *responsesError   `json:"error,omitempty"`
	Message  string            `json:"message,omitempty"`
	Code     string            `json:"code,omitempty"`
}

type responsesError struct {
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

type responsesOutputItem struct {
	ID               string                 `json:"id,omitempty"`
	Type             string                 `json:"type,omitempty"`
	Role             string                 `json:"role,omitempty"`
	Content          []responsesContentPart `json:"content,omitempty"`
	CallID           string                 `json:"call_id,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Arguments        string                 `json:"arguments,omitempty"`
	Summary          []responsesSummaryPart `json:"summary,omitempty"`
	EncryptedContent string                 `json:"encrypted_content,omitempty"`
}

type responsesUsage struct {
	InputTokens         int64 `json:"input_tokens,omitempty"`
	OutputTokens        int64 `json:"output_tokens,omitempty"`
	TotalTokens         int64 `json:"total_tokens,omitempty"`
	OutputTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	} `json:"output_tokens_details,omitempty"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens,omitempty"`
	} `json:"input_tokens_details,omitempty"`
}

// responsesPayload never sends max_output_tokens: the ChatGPT Codex backend
// rejects the parameter, so the model's own limit applies.
func (r *Runtime) responsesPayload(request providers.Request) responsesRequest {
	payload := responsesRequest{
		Model:             r.model,
		Input:             make([]responsesItem, 0, len(request.Messages)),
		Tools:             encodeResponsesTools(request.Tools),
		ParallelToolCalls: r.capabilities.ParallelToolCalls,
		Store:             false,
		PromptCacheKey:    strings.TrimSpace(request.CacheKey),
		Stream:            true,
	}
	payload.Instructions = combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions)
	if request.ToolChoice == providers.ToolChoiceNone && len(payload.Tools) > 0 {
		payload.ToolChoice = string(providers.ToolChoiceNone)
	}
	if r.reasoningEffort != "" && (len(payload.Tools) == 0 || r.capabilities.ReasoningWithTools) {
		payload.Reasoning = &responsesReasoning{Effort: r.reasoningEffort}
		payload.Include = []string{"reasoning.encrypted_content"}
	}
	for _, message := range request.Messages {
		payload.Input = append(payload.Input, responsesItemsFromMessage(message)...)
	}
	return payload
}

func responsesItemsFromMessage(message providers.Message) []responsesItem {
	role := strings.ToLower(strings.TrimSpace(message.Role))
	switch role {
	case "tool":
		callID := strings.TrimSpace(message.ToolCallID)
		if callID == "" {
			return nil
		}
		output := strings.TrimSpace(message.Content)
		if output == "" {
			output = "Tool execution completed without textual output."
		}
		return []responsesItem{{
			Type:   "function_call_output",
			CallID: callID,
			Output: output,
		}}
	case "assistant":
		items := responsesReasoningItems(message.Reasoning)
		if strings.TrimSpace(message.Content) != "" {
			items = append(items, responsesItem{
				Type: "message",
				Role: "assistant",
				Content: []responsesContentPart{{
					Type: "output_text",
					Text: message.Content,
				}},
			})
		}
		for i, call := range message.ToolCalls {
			name := strings.TrimSpace(call.Name)
			if name == "" {
				continue
			}
			callID := strings.TrimSpace(call.ID)
			if callID == "" {
				callID = fmt.Sprintf("call_%d", i)
			}
			items = append(items, responsesItem{
				Type:      "function_call",
				CallID:    callID,
				Name:      name,
				Arguments: responsesFunctionArguments(call.Arguments),
			})
		}
		return items
	default:
		parts := responsesContentParts(message, "input_text")
		if len(parts) == 0 {
			return nil
		}
		return []responsesItem{{
			Type:    "message",
			Role:    "user",
			Content: parts,
		}}
	}
}

// responsesReasoningItems replays encrypted reasoning; with store=false the server
// kept nothing, so the item is rebuilt without an id.
func responsesReasoningItems(blocks []providers.ReasoningBlock) []responsesItem {
	var items []responsesItem
	for _, block := range blocks {
		if block.RedactedData == "" {
			continue
		}
		summary := []responsesSummaryPart{}
		if block.Text != "" {
			summary = append(summary, responsesSummaryPart{Type: "summary_text", Text: block.Text})
		}
		items = append(items, responsesItem{Type: "reasoning", Summary: &summary, EncryptedContent: block.RedactedData})
	}
	return items
}

func responsesContentParts(message providers.Message, textType string) []responsesContentPart {
	parts := []responsesContentPart(nil)
	if text := strings.TrimSpace(message.Content); text != "" {
		parts = append(parts, responsesContentPart{Type: textType, Text: text})
	}
	for _, image := range message.Images {
		if data := strings.TrimSpace(image.DataBase64); data != "" {
			mimeType := strings.TrimSpace(image.MIMEType)
			if mimeType == "" {
				mimeType = "image/png"
			}
			parts = append(parts, responsesContentPart{
				Type:     "input_image",
				ImageURL: "data:" + mimeType + ";base64," + data,
			})
		}
	}
	return parts
}

func encodeResponsesTools(tools []providers.ToolDefinition) []responsesTool {
	out := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		params := tool.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        name,
			Description: strings.TrimSpace(tool.Description),
			Parameters:  params,
			Strict:      false,
		})
	}
	return out
}

func responsesFunctionArguments(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "{}"
	}
	return value
}

func responsesToolArguments(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(value)
}

func (usage responsesUsage) toProviderUsage() providers.Usage {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 &&
		usage.InputTokensDetails.CachedTokens == 0 && usage.OutputTokensDetails.ReasoningTokens == 0 {
		return providers.Usage{}
	}
	raw, _ := json.Marshal(usage)
	return providers.Usage{
		PromptTokens:    usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CacheReadTokens: usage.InputTokensDetails.CachedTokens,
		ReasoningTokens: usage.OutputTokensDetails.ReasoningTokens,
		ProviderRaw:     raw,
	}
}

func combinedSystemPrompt(systemPrompt string, customInstructions string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	customInstructions = strings.TrimSpace(customInstructions)
	switch {
	case systemPrompt == "" && customInstructions == "":
		return ""
	case systemPrompt == "":
		return customInstructions
	case customInstructions == "":
		return systemPrompt
	default:
		return systemPrompt + "\n\n" + customInstructions
	}
}
