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
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const defaultTimeout = 120 * time.Second

type Config struct {
	ProviderID      string
	CatalogID       string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	ReasoningEffort string
	ToolUseMode     providers.ToolUseMode
	Profile         providers.ProviderProfile
	HTTPClient      *http.Client
}

type Runtime struct {
	client          *http.Client
	baseURL         string
	model           string
	maxOutputTokens int64
	reasoningEffort string
	profile         providers.RuntimeProfile
	capabilities    providers.ModelCapabilities
}

func New(_ context.Context, cfg Config) (providers.Runtime, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
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
		maxOutputTokens: cfg.MaxOutputTokens,
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
	req.Header.Set("Accept", "application/json")
	res, err := r.client.Do(req)
	if err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openai-codex: read response: %w", err)
		}
		return providers.Response{}, fmt.Errorf("openai-codex: %s", decodeError(raw))
	}
	return r.decodeStream(ctx, res.Body)
}

type responsesRequest struct {
	Model             string              `json:"model"`
	Instructions      string              `json:"instructions,omitempty"`
	Input             []responsesItem     `json:"input"`
	Tools             []responsesTool     `json:"tools,omitempty"`
	MaxOutputTokens   *int64              `json:"max_output_tokens,omitempty"`
	Reasoning         *responsesReasoning `json:"reasoning,omitempty"`
	ParallelToolCalls bool                `json:"parallel_tool_calls,omitempty"`
	Store             bool                `json:"store"`
	Include           []string            `json:"include,omitempty"`
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
}

type responsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict"`
}

type responsesResponse struct {
	Output []responsesOutputItem `json:"output"`
	Usage  responsesUsage        `json:"usage,omitempty"`
}

type responsesStreamEvent struct {
	Type        string              `json:"type,omitempty"`
	Delta       string              `json:"delta,omitempty"`
	ItemID      string              `json:"item_id,omitempty"`
	OutputIndex int                 `json:"output_index,omitempty"`
	Item        responsesOutputItem `json:"item,omitempty"`
	Response    responsesResponse   `json:"response,omitempty"`
	Usage       responsesUsage      `json:"usage,omitempty"`
	Error       *responsesError     `json:"error,omitempty"`
}

type responsesError struct {
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

type responsesOutputItem struct {
	ID        string                 `json:"id,omitempty"`
	Type      string                 `json:"type,omitempty"`
	Role      string                 `json:"role,omitempty"`
	Content   []responsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
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

func (r *Runtime) responsesPayload(request providers.Request) responsesRequest {
	payload := responsesRequest{
		Model:             r.model,
		Input:             make([]responsesItem, 0, len(request.Messages)),
		Tools:             encodeResponsesTools(request.Tools),
		ParallelToolCalls: r.capabilities.ParallelToolCalls,
		Store:             false,
		Stream:            true,
	}
	payload.Instructions = combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions)
	if r.maxOutputTokens > 0 {
		payload.MaxOutputTokens = &r.maxOutputTokens
	}
	if r.reasoningEffort != "" && (len(payload.Tools) == 0 || r.capabilities.ReasoningWithTools) {
		payload.Reasoning = &responsesReasoning{Effort: r.reasoningEffort}
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
		items := []responsesItem(nil)
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

func (r *Runtime) decodeResponse(raw []byte) (providers.Response, error) {
	var response responsesResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: decode response: %w", err)
	}
	var text strings.Builder
	toolCalls := []providers.ToolCall(nil)
	for _, item := range response.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" || part.Type == "text" {
					text.WriteString(part.Text)
				}
			}
		case "function_call":
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				callID = strings.TrimSpace(item.ID)
			}
			toolCalls = append(toolCalls, providers.ToolCall{
				ID:        callID,
				Name:      name,
				Arguments: responsesToolArguments(item.Arguments),
			})
		}
	}
	reply := strings.TrimSpace(text.String())
	if reply == "" && len(toolCalls) == 0 {
		return providers.Response{}, errors.New("openai-codex: empty assistant reply")
	}
	return providers.Response{
		Text:      reply,
		Model:     r.model,
		Provider:  providers.TypeOpenAICodex,
		ToolCalls: toolCalls,
		Usage:     response.Usage.toProviderUsage(),
	}, nil
}

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var text strings.Builder
	toolCalls := map[string]responsesOutputItem{}
	var final responsesResponse
	var usage responsesUsage
	if err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "[DONE]" {
			return nil
		}
		var chunk responsesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("openai-codex: decode stream event: %w", err)
		}
		eventType := strings.TrimSpace(chunk.Type)
		if eventType == "" {
			eventType = strings.TrimSpace(event.Type)
		}
		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return fmt.Errorf("openai-codex: %s", strings.TrimSpace(chunk.Error.Message))
		}
		if strings.HasSuffix(eventType, "output_text.delta") && chunk.Delta != "" {
			text.WriteString(chunk.Delta)
			return providers.StreamText(ctx, chunk.Delta)
		}
		if strings.HasSuffix(eventType, "function_call_arguments.delta") && chunk.Delta != "" {
			key := streamToolCallKey(chunk.ItemID, chunk.OutputIndex)
			item := toolCalls[key]
			item.ID = firstNonEmpty(item.ID, chunk.ItemID)
			item.Type = "function_call"
			item.Arguments += chunk.Delta
			toolCalls[key] = item
			return nil
		}
		if (strings.HasSuffix(eventType, "output_item.done") || strings.HasSuffix(eventType, "output_item.added")) && chunk.Item.Type == "function_call" {
			key := streamToolCallKey(firstNonEmpty(chunk.Item.ID, chunk.Item.CallID, chunk.ItemID), chunk.OutputIndex)
			item := toolCalls[key]
			item.ID = firstNonEmpty(chunk.Item.ID, item.ID)
			item.CallID = firstNonEmpty(chunk.Item.CallID, item.CallID)
			item.Name = firstNonEmpty(chunk.Item.Name, item.Name)
			if strings.TrimSpace(chunk.Item.Arguments) != "" {
				item.Arguments = chunk.Item.Arguments
			}
			item.Type = "function_call"
			toolCalls[key] = item
			return nil
		}
		if strings.HasSuffix(eventType, "completed") {
			final = chunk.Response
			if chunk.Response.Usage.TotalTokens > 0 || chunk.Response.Usage.InputTokens > 0 || chunk.Response.Usage.OutputTokens > 0 {
				usage = chunk.Response.Usage
			}
			return nil
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.InputTokens > 0 || chunk.Usage.OutputTokens > 0 {
			usage = chunk.Usage
		}
		return nil
	}); err != nil {
		return providers.Response{}, err
	}
	reply := strings.TrimSpace(text.String())
	if reply == "" && len(final.Output) > 0 {
		fallbackRaw, _ := json.Marshal(final)
		fallback, err := r.decodeResponse(fallbackRaw)
		if err == nil {
			return fallback, nil
		}
	}
	calls := streamResponsesToolCalls(toolCalls)
	if reply == "" && len(calls) == 0 {
		return providers.Response{}, errors.New("openai-codex: empty assistant reply")
	}
	return providers.Response{
		Text:      reply,
		Model:     r.model,
		Provider:  providers.TypeOpenAICodex,
		ToolCalls: calls,
		Usage:     usage.toProviderUsage(),
	}, nil
}

func streamToolCallKey(itemID string, outputIndex int) string {
	if itemID = strings.TrimSpace(itemID); itemID != "" {
		return itemID
	}
	return fmt.Sprintf("index:%d", outputIndex)
}

func streamResponsesToolCalls(items map[string]responsesOutputItem) []providers.ToolCall {
	if len(items) == 0 {
		return nil
	}
	out := make([]providers.ToolCall, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		out = append(out, providers.ToolCall{
			ID:        firstNonEmpty(item.CallID, item.ID),
			Name:      strings.TrimSpace(item.Name),
			Arguments: responsesToolArguments(item.Arguments),
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
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		TotalTokens:     usage.TotalTokens,
		CachedTokens:    usage.InputTokensDetails.CachedTokens,
		ReasoningTokens: usage.OutputTokensDetails.ReasoningTokens,
		ProviderRaw:     raw,
	}
}

func combinedSystemPrompt(systemPrompt string, customInstructions string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	customInstructions = strings.TrimSpace(customInstructions)
	switch {
	case systemPrompt == "" && customInstructions == "":
		return "You are a helpful assistant. Respond in the same language as the user."
	case systemPrompt == "":
		return customInstructions
	case customInstructions == "":
		return systemPrompt
	default:
		return systemPrompt + "\n\n" + customInstructions
	}
}
