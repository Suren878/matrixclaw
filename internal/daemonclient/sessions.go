package daemonclient

import (
	"context"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
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

func (c *Client) ListSessions(ctx context.Context) ([]core.Session, error) {
	var response core.SessionsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions", nil, &response); err != nil {
		return nil, err
	}
	return response.Sessions, nil
}

func (c *Client) CreateSession(ctx context.Context, title string, workingDir string) (core.Session, error) {
	var response core.SessionResponse
	request := core.CreateSessionRequest{
		Title:      strings.TrimSpace(title),
		WorkingDir: strings.TrimSpace(workingDir),
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", request, &response); err != nil {
		return core.Session{}, err
	}
	return response.Session, nil
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

func (c *Client) CompactSession(ctx context.Context, sessionID string) (core.CompactSessionResult, error) {
	var response core.SessionCompactResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/compact"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return core.CompactSessionResult{}, err
	}
	return response.Compact, nil
}

func (c *Client) CreateSystemMessage(ctx context.Context, sessionID string, content string) (core.Message, error) {
	var response core.MessageResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/system-message"
	request := core.CreateSystemMessageRequest{Content: strings.TrimSpace(content)}
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return core.Message{}, err
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
	return c.SendMessageParts(ctx, sessionID, text, nil, workingDir)
}

func (c *Client) SendMessageParts(ctx context.Context, sessionID string, text string, parts []core.MessagePart, workingDir string) (core.AcceptRunResult, error) {
	var response core.AcceptRunResult
	request := core.HandleMessageInput{
		Client:           c.ClientName,
		ExternalKey:      c.ExternalKey,
		SessionID:        strings.TrimSpace(sessionID),
		Text:             text,
		Parts:            parts,
		WorkingDir:       strings.TrimSpace(workingDir),
		AllowAutoBindOne: true,
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/messages", request, &response); err != nil {
		return core.AcceptRunResult{}, err
	}
	return response, nil
}

func (c *Client) ModelsForSession(ctx context.Context, sessionID string) (string, string, []string, error) {
	var response core.SessionModelsResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/models"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return "", "", nil, err
	}
	return response.ProviderID, response.ModelID, response.Models, nil
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
