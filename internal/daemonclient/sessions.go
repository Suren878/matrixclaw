package daemonclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Client) CurrentBinding(ctx context.Context) (core.ClientBinding, error) {
	path := "/v1/bindings/current?" + c.bindingQuery()
	var response core.ClientBindingResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.ClientBinding{}, err
	}
	return response.Binding, nil
}

func (c *Client) LoadSnapshot(ctx context.Context) (core.ClientSnapshot, error) {
	path := "/v1/snapshot?" + c.bindingQuery()
	var response core.ClientSnapshotResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.ClientSnapshot{}, err
	}
	return response.Snapshot, nil
}

func (c *Client) ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error) {
	values := url.Values{}
	values.Set("session_id", strings.TrimSpace(sessionID))
	values.Set("limit", strconv.Itoa(limit))
	var response core.MessagesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/messages?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	return response.Messages, nil
}

// ListMessagesAfter returns up to limit messages (all when limit is 0) with seq
// above afterSeq, oldest first.
func (c *Client) ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error) {
	values := url.Values{}
	values.Set("session_id", strings.TrimSpace(sessionID))
	values.Set("after_seq", strconv.FormatInt(afterSeq, 10))
	values.Set("limit", strconv.Itoa(limit))
	var response core.MessagesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/messages?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	return response.Messages, nil
}

func (c *Client) ListSessions(ctx context.Context) ([]core.Session, error) {
	var response core.SessionsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions", nil, &response); err != nil {
		return nil, err
	}
	return response.Sessions, nil
}

func (c *Client) GetSession(ctx context.Context, sessionID string) (core.Session, error) {
	var response core.SessionResponse
	path := "/v1/sessions/" + escapedPath(sessionID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
}

func (c *Client) CreateSession(ctx context.Context, title string, workingDir string) (core.Session, error) {
	return c.CreateSessionWithRequest(ctx, core.CreateSessionRequest{
		Title:      title,
		WorkingDir: workingDir,
	})
}

func (c *Client) CreateSessionWithRequest(ctx context.Context, request core.CreateSessionRequest) (core.Session, error) {
	var response core.SessionResponse
	request.Title = strings.TrimSpace(request.Title)
	request.Kind = strings.TrimSpace(request.Kind)
	request.RuntimeID = strings.TrimSpace(request.RuntimeID)
	request.WorkingDir = strings.TrimSpace(request.WorkingDir)
	request.ProviderID = strings.TrimSpace(request.ProviderID)
	request.ModelID = strings.TrimSpace(request.ModelID)
	request.PermissionMode = strings.TrimSpace(request.PermissionMode)
	request.ExternalAgentID = strings.TrimSpace(request.ExternalAgentID)
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", request, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
}

func (c *Client) ListExternalAgents(ctx context.Context) ([]core.ExternalAgentDescriptor, error) {
	var response core.ExternalAgentsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/external-agents", nil, &response); err != nil {
		return nil, err
	}
	return response.Agents, nil
}

func (c *Client) UpdateExternalAgent(ctx context.Context, agentID string, update core.UpdateExternalAgentRequest) ([]core.ExternalAgentDescriptor, error) {
	var response core.ExternalAgentsResponse
	path := "/v1/external-agents/" + escapedPath(agentID)
	if err := c.doJSON(ctx, http.MethodPatch, path, update, &response); err != nil {
		return nil, err
	}
	return response.Agents, nil
}

func (c *Client) RenameSession(ctx context.Context, sessionID string, title string) (core.Session, error) {
	var response core.SessionResponse
	path := "/v1/sessions/" + escapedPath(sessionID)
	request := core.RenameSessionRequest{Title: strings.TrimSpace(title)}
	if err := c.doJSON(ctx, http.MethodPatch, path, request, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
}

func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	path := "/v1/sessions/" + escapedPath(sessionID)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) SessionContext(ctx context.Context, sessionID string) (core.ContextReport, error) {
	var response core.SessionContextResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/context"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.ContextReport{}, err
	}
	return response.Context, nil
}

func (c *Client) SessionUsage(ctx context.Context, sessionID string) (core.UsageReport, error) {
	var response core.UsageResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/usage"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.UsageReport{}, err
	}
	return response.Usage, nil
}

func (c *Client) SessionBudget(ctx context.Context, sessionID string) (core.SessionBudgetReport, error) {
	var response core.SessionBudgetResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/budget"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.SessionBudgetReport{}, err
	}
	return response.Budget, nil
}

func (c *Client) UpdateSessionBudget(ctx context.Context, sessionID string, budget core.SessionBudget) (core.SessionBudgetReport, error) {
	var response core.SessionBudgetResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/budget"
	if err := c.doJSON(ctx, http.MethodPut, path, budget, &response); err != nil {
		return core.SessionBudgetReport{}, err
	}
	return response.Budget, nil
}

func (c *Client) CompactSession(ctx context.Context, sessionID string) (core.CompactSessionResult, error) {
	var response core.SessionCompactResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/compact"
	if err := c.doJSONWithClient(ctx, http.MethodPost, path, nil, &response, c.compactHTTPClient()); err != nil {
		return core.CompactSessionResult{}, err
	}
	return response.Compact, nil
}

func (c *Client) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	var response core.MessageResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/clear"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return transcript.Message{}, err
	}
	return response.Message, nil
}

func (c *Client) CreateSystemMessage(ctx context.Context, sessionID string, content string) (transcript.Message, error) {
	var response core.MessageResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/system-message"
	request := core.CreateSystemMessageRequest{Content: strings.TrimSpace(content)}
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return transcript.Message{}, err
	}
	return response.Message, nil
}

func (c *Client) UseSession(ctx context.Context, sessionID string) (core.ClientBinding, error) {
	var response core.ClientBindingResponse
	request := core.UseBindingInput{
		Client:      c.ClientName,
		ExternalKey: c.ExternalKey,
		SessionID:   strings.TrimSpace(sessionID),
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/bindings/use", request, &response); err != nil {
		return core.ClientBinding{}, err
	}
	return response.Binding, nil
}

func (c *Client) SendMessage(ctx context.Context, sessionID string, text string, workingDir string) (core.AcceptRunResult, error) {
	return c.SendMessagePartsMode(ctx, sessionID, text, nil, workingDir, "")
}

func (c *Client) SendMessageMode(ctx context.Context, sessionID string, text string, workingDir string, busyMode core.BusyInputMode) (core.AcceptRunResult, error) {
	return c.SendMessagePartsMode(ctx, sessionID, text, nil, workingDir, busyMode)
}

func (c *Client) SendMessagePartsMode(ctx context.Context, sessionID string, text string, parts []transcript.MessagePart, workingDir string, busyMode core.BusyInputMode) (core.AcceptRunResult, error) {
	return c.SendMessagePartsModeWithDelivery(ctx, sessionID, text, parts, workingDir, busyMode, nil, false)
}

// SendMessagePartsModeWithDelivery sends a message whose run is delivered to
// deliveryAddress; replyOnce marks an address that takes one reply only.
func (c *Client) SendMessagePartsModeWithDelivery(ctx context.Context, sessionID string, text string, parts []transcript.MessagePart, workingDir string, busyMode core.BusyInputMode, deliveryAddress json.RawMessage, replyOnce bool) (core.AcceptRunResult, error) {
	var response core.AcceptRunResult
	request := core.HandleMessageInput{
		Client:             c.ClientName,
		ExternalKey:        c.ExternalKey,
		ClientCapabilities: c.Capabilities,
		SessionID:          strings.TrimSpace(sessionID),
		Text:               text,
		Parts:              parts,
		BusyMode:           busyMode,
		WorkingDir:         strings.TrimSpace(workingDir),
		DeliveryAddress:    deliveryAddress,
		ReplyOnce:          replyOnce,
		AllowAutoBindOne:   true,
		Restricted:         c.Restricted,
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/messages", request, &response); err != nil {
		return core.AcceptRunResult{}, err
	}
	return response, nil
}

// ContinueSession starts a run that continues the session's latest run with a
// fresh budget.
func (c *Client) ContinueSession(ctx context.Context, sessionID string, workingDir string) (core.AcceptRunResult, error) {
	var response core.AcceptRunResult
	request := core.HandleMessageInput{
		Client:             c.ClientName,
		ExternalKey:        c.ExternalKey,
		ClientCapabilities: c.Capabilities,
		SessionID:          strings.TrimSpace(sessionID),
		WorkingDir:         strings.TrimSpace(workingDir),
		AllowAutoBindOne:   true,
		Restricted:         c.Restricted,
		Continue:           true,
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/messages", request, &response); err != nil {
		return core.AcceptRunResult{}, err
	}
	return response, nil
}

func (c *Client) ListSessionProviders(ctx context.Context) ([]core.SessionProviderOption, error) {
	var response core.SessionProvidersResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/session-providers", nil, &response); err != nil {
		return nil, err
	}
	return response.Providers, nil
}

func (c *Client) UpdateSessionProvider(ctx context.Context, sessionID string, providerID string) (core.Session, error) {
	var response core.SessionResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/llm"
	request := core.UpdateSessionLLMRequest{ProviderID: strings.TrimSpace(providerID)}
	if err := c.doJSON(ctx, http.MethodPatch, path, request, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
}

func (c *Client) SessionModels(ctx context.Context, sessionID string) (core.SessionModelsResponse, error) {
	var response core.SessionModelsResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/models"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.SessionModelsResponse{}, err
	}
	return response, nil
}

func (c *Client) UpdateSessionModel(ctx context.Context, sessionID string, modelID string) (core.Session, error) {
	var response core.SessionResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/llm"
	request := core.UpdateSessionLLMRequest{ModelID: strings.TrimSpace(modelID)}
	if err := c.doJSON(ctx, http.MethodPatch, path, request, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
}

func (c *Client) UpdateSessionPermissionMode(ctx context.Context, sessionID string, mode core.PermissionMode) (core.Session, error) {
	var response core.SessionResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/permissions"
	request := core.UpdateSessionPermissionModeRequest{PermissionMode: string(core.NormalizePermissionMode(string(mode)))}
	if err := c.doJSON(ctx, http.MethodPatch, path, request, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
}
