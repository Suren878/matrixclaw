package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func (c *Core) CreateSession(ctx context.Context, input CreateSessionInput) (Session, error) {
	title := normalizeText(input.Title)
	if title == "" {
		return Session{}, fmt.Errorf("%w: session title is required", ErrInvalidInput)
	}
	runtimeID := sessionRuntimeForCreate(input.RuntimeID, input.Kind, input.ExternalAgentID)
	kind := sessionKindForRuntime(runtimeID)
	workingDir := normalizeWorkingDir(input.WorkingDir)
	providerID := normalizeText(input.ProviderID)
	modelID := normalizeText(input.ModelID)
	rawPermissionMode := normalizeText(string(input.PermissionMode))
	permissionMode := NormalizePermissionMode(string(input.PermissionMode))
	if kind == SessionKindExternalAgent && rawPermissionMode == "" {
		permissionMode = PermissionModeFullAuto
	}
	if kind == SessionKindAssistant {
		llms := c.sessionLLMs()
		if llms != nil {
			var err error
			_, modelID, err = llms.Normalize(providerID, modelID)
			if err != nil {
				return Session{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			if providerID == "" {
				providerID, _ = llms.ActiveSelection()
			}
		}
	} else {
		providerID = ""
		modelID = normalizeText(input.ModelID)
	}

	now := c.now().UTC()
	session := Session{
		ID:             c.newID("session"),
		Title:          title,
		Kind:           kind,
		RuntimeID:      runtimeID,
		WorkingDir:     workingDir,
		ProviderID:     providerID,
		ModelID:        modelID,
		PermissionMode: permissionMode,
		Status:         SessionStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := c.store.CreateSession(ctx, session); err != nil {
		return Session{}, err
	}
	if kind == SessionKindExternalAgent {
		if err := c.createExternalAgentAttachment(ctx, session, input); err != nil {
			_ = c.store.DeleteSession(ctx, session.ID)
			return Session{}, err
		}
	}
	return session, nil
}

func (c *Core) ListSessions(ctx context.Context, filter SessionListFilter) ([]Session, error) {
	sessions, err := c.store.ListSessions(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		sessions[i] = c.decorateSessionLLM(sessions[i])
	}
	return sessions, nil
}

func (c *Core) RenameSession(ctx context.Context, input RenameSessionInput) (Session, error) {
	sessionID := normalizeText(input.SessionID)
	if sessionID == "" {
		return Session{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	title := normalizeText(input.Title)
	if title == "" {
		return Session{}, fmt.Errorf("%w: session title is required", ErrInvalidInput)
	}

	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	session.Title = title
	session.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateSession(ctx, session); err != nil {
		return Session{}, err
	}
	return c.decorateSessionLLM(session), nil
}

func (c *Core) UpdateSessionPermissionMode(ctx context.Context, input UpdateSessionPermissionModeInput) (Session, error) {
	sessionID := normalizeText(input.SessionID)
	if sessionID == "" {
		return Session{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	session = c.decorateSessionLLM(session)
	session.PermissionMode = NormalizePermissionMode(string(input.PermissionMode))
	session.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateSession(ctx, session); err != nil {
		return Session{}, err
	}
	if session.Kind == SessionKindExternalAgent && c.externalStore != nil {
		if attachment, err := c.externalStore.GetExternalAgentSession(ctx, session.ID); err == nil {
			approvalPolicy, sandbox := externalAgentPolicyForPermissionMode(session.PermissionMode)
			attachment.ApprovalPolicy = approvalPolicy
			attachment.Sandbox = sandbox
			attachment.UpdatedAt = session.UpdatedAt
			if err := c.externalStore.SaveExternalAgentSession(ctx, attachment); err != nil {
				return Session{}, err
			}
		} else if !errors.Is(err, ErrNotFound) {
			return Session{}, err
		}
	}
	return c.decorateSessionLLM(session), nil
}

func (c *Core) DeleteSession(ctx context.Context, sessionID string) error {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.DeleteSession(ctx, sessionID)
}

func (c *Core) resolveSession(ctx context.Context, input HandleMessageInput) (Session, error) {
	workingDir := normalizeWorkingDir(input.WorkingDir)

	if input.SessionID != "" {
		session, err := c.store.GetSession(ctx, input.SessionID)
		if err != nil {
			return Session{}, err
		}
		session = c.decorateSessionLLM(session)
		if workingDir != "" && workingDir != session.WorkingDir {
			session.WorkingDir = workingDir
			session.UpdatedAt = c.now().UTC()
			if err := c.store.UpdateSession(ctx, session); err != nil {
				return Session{}, err
			}
		}
		if input.Client != "" && input.ExternalKey != "" {
			if _, err := c.UseBinding(ctx, UseBindingInput{
				Client:      input.Client,
				ExternalKey: input.ExternalKey,
				SessionID:   input.SessionID,
			}); err != nil {
				return Session{}, err
			}
		}
		return session, nil
	}

	if input.Client == "" || input.ExternalKey == "" {
		return Session{}, ErrSessionSelectionRequired
	}

	binding, err := c.store.GetBinding(ctx, input.Client, input.ExternalKey)
	if err == nil {
		session, err := c.store.GetSession(ctx, binding.SessionID)
		if err != nil {
			return Session{}, err
		}
		session = c.decorateSessionLLM(session)
		if workingDir != "" && workingDir != session.WorkingDir {
			session.WorkingDir = workingDir
			session.UpdatedAt = c.now().UTC()
			if err := c.store.UpdateSession(ctx, session); err != nil {
				return Session{}, err
			}
		}
		return session, nil
	}
	if !errors.Is(err, ErrBindingNotFound) && !errors.Is(err, ErrNotFound) {
		return Session{}, err
	}

	if !input.AllowAutoBindOne {
		return Session{}, ErrSessionSelectionRequired
	}

	sessions, err := c.store.ListSessions(ctx, SessionListFilter{})
	if err != nil {
		return Session{}, err
	}
	if len(sessions) != 1 {
		return Session{}, ErrSessionSelectionRequired
	}
	if _, err := c.UseBinding(ctx, UseBindingInput{
		Client:      input.Client,
		ExternalKey: input.ExternalKey,
		SessionID:   sessions[0].ID,
	}); err != nil {
		return Session{}, err
	}
	session := sessions[0]
	session = c.decorateSessionLLM(session)
	if workingDir != "" && workingDir != session.WorkingDir {
		session.WorkingDir = workingDir
		session.UpdatedAt = c.now().UTC()
		if err := c.store.UpdateSession(ctx, session); err != nil {
			return Session{}, err
		}
	}
	return session, nil
}

func (c *Core) UpdateSessionProvider(ctx context.Context, sessionID string, providerID string) (Session, error) {
	sessionID = normalizeText(sessionID)
	providerID = normalizeText(providerID)
	if sessionID == "" || providerID == "" {
		return Session{}, fmt.Errorf("%w: session id and provider id are required", ErrInvalidInput)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	session = c.decorateSessionLLM(session)
	llms := c.sessionLLMs()
	if llms == nil {
		return Session{}, fmt.Errorf("%w: provider registry unavailable", ErrExecutionUnavailable)
	}
	_, modelID, err := llms.Normalize(providerID, "")
	if err != nil {
		return Session{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	session.ProviderID = providerID
	session.ModelID = modelID
	session.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateSession(ctx, session); err != nil {
		return Session{}, err
	}
	return c.decorateSessionLLM(session), nil
}

func (c *Core) UpdateSessionModel(ctx context.Context, sessionID string, modelID string) (Session, error) {
	sessionID = normalizeText(sessionID)
	modelID = normalizeText(modelID)
	if sessionID == "" || modelID == "" {
		return Session{}, fmt.Errorf("%w: session id and model id are required", ErrInvalidInput)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	session = c.decorateSessionLLM(session)
	llms := c.sessionLLMs()
	if llms == nil {
		return Session{}, fmt.Errorf("%w: provider registry unavailable", ErrExecutionUnavailable)
	}
	providerID := normalizeText(session.ProviderID)
	if providerID == "" {
		providerID, _ = llms.ActiveSelection()
	}
	_, resolvedModel, err := llms.Normalize(providerID, modelID)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	session.ProviderID = providerID
	session.ModelID = resolvedModel
	session.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateSession(ctx, session); err != nil {
		return Session{}, err
	}
	return c.decorateSessionLLM(session), nil
}

func (c *Core) SessionProviderOptions() []SessionProviderOption {
	llms := c.sessionLLMs()
	if llms == nil {
		return nil
	}
	return llms.Providers()
}

func (c *Core) ModelsForSession(ctx context.Context, sessionID string) (string, string, []string, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return "", "", nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", "", nil, err
	}
	session = c.decorateSessionLLM(session)
	llms := c.sessionLLMs()
	if llms == nil {
		return session.ProviderID, session.ModelID, nil, fmt.Errorf("%w: provider registry unavailable", ErrExecutionUnavailable)
	}
	models, err := llms.Models(ctx, session.ProviderID)
	if err != nil {
		return session.ProviderID, session.ModelID, nil, err
	}
	return session.ProviderID, session.ModelID, models, nil
}

func (c *Core) decorateSessionLLM(session Session) Session {
	session.Kind = NormalizeSessionKind(session.Kind)
	if session.Kind == SessionKindExternalAgent && (session.RuntimeID == "" || NormalizeSessionRuntime(session.RuntimeID) == SessionRuntimeMatrixClaw) {
		session.RuntimeID = SessionRuntimeExternalAgent
	}
	if session.RuntimeID == "" {
		session.RuntimeID = SessionRuntimeMatrixClaw
	}
	session.RuntimeID = NormalizeSessionRuntime(session.RuntimeID)
	session.PermissionMode = NormalizePermissionMode(string(session.PermissionMode))
	if session.Kind == SessionKindExternalAgent {
		return session
	}
	llms := c.sessionLLMs()
	if llms == nil {
		return session
	}
	providerID, modelID := llms.ActiveSelection()
	if strings.TrimSpace(session.ProviderID) == "" {
		session.ProviderID = providerID
	}
	if strings.TrimSpace(session.ModelID) == "" {
		session.ModelID = modelID
	}
	if strings.TrimSpace(session.ProviderID) != "" {
		if _, resolvedModel, err := llms.Normalize(session.ProviderID, session.ModelID); err == nil {
			session.ModelID = resolvedModel
		}
	}
	return session
}

func normalizeWorkingDir(value string) string {
	value = normalizeText(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}
