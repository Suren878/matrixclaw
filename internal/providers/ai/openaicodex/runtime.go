package openaicodex

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// providerID is the catalog id of the Codex subscription provider.
const providerID = "openai-codex"

type Runtime struct {
	providers.RuntimeBase
	reasoningEffort string
}

func New(_ context.Context, cfg providers.RuntimeConfig) (providers.Runtime, error) {
	cfg.ProviderID = cmp.Or(cfg.ProviderID, providerID)
	cfg.BaseURL = strings.TrimRight(textutil.FirstNonEmpty(cfg.BaseURL, DefaultBaseURL), "/")
	base := providers.NewRuntimeBase(cfg, providers.TypeOpenAICodex, providers.DefaultOpenAICodexModel)
	reasoningEffort := ""
	if base.Capabilities.ReasoningEffort {
		reasoningEffort = providers.NormalizeReasoningEffort(cfg.ReasoningEffort)
		if reasoningEffort == providers.ReasoningEffortNone {
			reasoningEffort = ""
		}
	}
	return &Runtime{RuntimeBase: base, reasoningEffort: reasoningEffort}, nil
}

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	payload := r.responsesPayload(request)
	if len(payload.Input) == 0 {
		return providers.Response{}, errors.New("openai-codex: no input")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return providers.Response{}, fmt.Errorf("openai-codex: marshal request: %w", err)
	}
	res, err := send(ctx, r.Client, r.BaseURL, func(creds Credentials) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(creds.BaseURL, "/")+"/responses?client_version="+codexClientVersion, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("openai-codex: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		return req, nil
	})
	if err != nil {
		return providers.Response{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "application/json") {
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openai-codex: read response: %w", err)
		}
		return r.decodeResponse(raw)
	}
	return r.decodeStream(ctx, res.Body)
}

// send does the request build makes with current credentials. A 401 renews
// them once and sends again; any other non-2xx status is an *APIError.
func send(ctx context.Context, client *http.Client, baseURL string, build func(Credentials) (*http.Request, error)) (*http.Response, error) {
	creds, err := ResolveCredentials(ctx, client, baseURL)
	if err != nil {
		return nil, err
	}
	for retried := false; ; retried = true {
		req, err := build(creds)
		if err != nil {
			return nil, err
		}
		setCodexHeaders(req, creds.AccessToken)
		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("openai-codex: request failed: %w", err)
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res, nil
		}
		raw, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("openai-codex: read response: %w", err)
		}
		parsed := parseError(raw)
		if res.StatusCode != http.StatusUnauthorized {
			apiErr := providers.NewAPIError(providerID, res.StatusCode, parsed.Code, parsed.Message, res.Header)
			apiErr.RetryAfter = max(apiErr.RetryAfter, parsed.RetryAfter)
			return nil, apiErr
		}
		if retried {
			return nil, reauthRequired("the backend rejected renewed credentials: " + parsed.Message)
		}
		if creds, err = refreshRejected(ctx, client, creds); err != nil {
			return nil, err
		}
	}
}

func setCodexHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", "codex_cli_rs/0.0.0 (Matrixclaw)")
	req.Header.Set("originator", "codex_cli_rs")
	if accountID := jwtChatGPTAccountID(accessToken); accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}
}

// codexError is an error body: OpenAI style, OAuth style or a bare message.
// RetryAfter is when a plan's usage limit resets.
type codexError struct {
	Code       string
	Message    string
	RetryAfter time.Duration
}

func parseError(raw []byte) codexError {
	var payload struct {
		Error *struct {
			Type            string      `json:"type"`
			Code            string      `json:"code"`
			Message         string      `json:"message"`
			ResetsInSeconds json.Number `json:"resets_in_seconds"`
		} `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
		Detail           string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if e := payload.Error; e != nil && strings.TrimSpace(e.Message) != "" {
			return codexError{
				Code:       textutil.FirstNonEmpty(e.Code, e.Type),
				Message:    strings.TrimSpace(e.Message),
				RetryAfter: providers.ParseRetryDelay(e.ResetsInSeconds.String()),
			}
		}
		if message := textutil.FirstNonEmpty(payload.ErrorDescription, payload.Message, payload.Detail); message != "" {
			return codexError{Message: message}
		}
	}
	return codexError{Message: textutil.FirstNonEmpty(strings.TrimSpace(string(raw)), "unknown error")}
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
		Model:             r.Model,
		Input:             make([]responsesItem, 0, len(request.Messages)),
		Tools:             encodeResponsesTools(request.Tools),
		ParallelToolCalls: r.Capabilities.ToolCalling,
		Store:             false,
		PromptCacheKey:    strings.TrimSpace(request.CacheKey),
		Stream:            true,
	}
	payload.Instructions = strings.TrimSpace(request.SystemPrompt)
	if request.ToolChoice == providers.ToolChoiceNone && len(payload.Tools) > 0 {
		payload.ToolChoice = string(providers.ToolChoiceNone)
	}
	if r.reasoningEffort != "" && (len(payload.Tools) == 0 || r.Capabilities.ToolCalling) {
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
