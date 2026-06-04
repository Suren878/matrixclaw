package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleSessions(ctx context.Context, externalKey string) (Result, error) {
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	currentSessionID, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	sessions, err := d.sessions.ListSessions(ctx)
	if err != nil {
		return Result{}, err
	}
	if session != nil {
		currentSessionID = session.ID
	}
	picker := NewPickerData(PickerSessions, "Sessions")
	picker.Item(PickerItem{
		ID:      "new",
		Title:   "New Session",
		Command: newSessionCommand(),
		Role:    PickerItemRoleAction,
	})
	for _, session := range sessions {
		title := strings.TrimSpace(session.Title)
		if title == "" {
			title = strings.TrimSpace(session.ID)
		}
		info := sessionListInfo(session)
		selected := strings.TrimSpace(session.ID) == strings.TrimSpace(currentSessionID)
		if selected {
			info = joinSessionInfo("Active", info)
		}
		picker.Item(PickerItem{
			ID:       session.ID,
			Title:    title,
			Info:     info,
			Selected: selected,
			Command:  sessionMenuCommand(session.ID),
		})
	}
	return Result{
		Handled: true,
		Picker:  picker.Ptr(),
	}, nil
}

func joinSessionInfo(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, " · ")
}

func (d *Dispatcher) handleNewSession(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.sessionRuntimePicker(ctx)
	}
	return d.createSession(ctx, externalKey, sessionTarget{runtimeID: core.SessionRuntimeMatrixClaw}, args)
}

func (d *Dispatcher) sessionRuntimePicker(ctx context.Context) (Result, error) {
	picker := NewPickerData(PickerSessionRuntime, "New Session").
		Back(sessionsCommand()).
		Row("matrixclaw", "Matrixclaw", "Built-in", sessionNewCommand("matrixclaw"))
	if d.externalAgents != nil {
		agents, err := d.externalAgents.ListExternalAgents(ctx)
		if err != nil {
			return Result{}, err
		}
		for _, agent := range agents {
			if !agent.Installed || !agent.Enabled {
				continue
			}
			picker.Row(agent.ID, externalAgentTitle(agent), externalAgentSessionRuntimeInfo(agent), sessionNewCommand(agent.ID))
		}
	}
	return Result{
		Handled: true,
		Picker:  picker.Ptr(),
	}, nil
}

func externalAgentSessionRuntimeInfo(agent core.ExternalAgentDescriptor) string {
	return "External"
}

func (d *Dispatcher) handleSession(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.handleSessions(ctx, externalKey)
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "new":
		return d.handleSessionNew(ctx, externalKey, rest)
	case "menu":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session menu <id>"}, nil
		}
		return d.handleSessionMenu(ctx, externalKey, sessionID)
	case "use":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session use <id>"}, nil
		}
		binding, err := d.sessions.UseSession(ctx, externalKey, sessionID)
		if err != nil {
			return Result{}, err
		}
		_, session, err := d.currentSession(ctx, externalKey)
		if err != nil {
			return Result{}, err
		}
		text := "Current session id: " + binding.SessionID
		if session != nil {
			text = "Current session: " + formatSessionLabel(*session, true)
		}
		return Result{
			Handled:        true,
			Text:           text,
			ReloadSnapshot: true,
		}, nil
	case "current":
		return d.handleCurrent(ctx, externalKey)
	case "rename":
		sessionID, title := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session rename <id> [title]"}, nil
		}
		return d.handleSessionRename(ctx, sessionID, title)
	case "model":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session model <id>"}, nil
		}
		return d.handleSessionModel(ctx, sessionID)
	case "set-model":
		sessionID, modelID := firstCommandToken(rest)
		if sessionID == "" || strings.TrimSpace(modelID) == "" {
			return Result{Handled: true, Text: "Usage: /session set-model <id> <model>"}, nil
		}
		return d.handleSessionSetModel(ctx, sessionID, modelID)
	case "delete":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session delete <id>"}, nil
		}
		return d.handleSessionDelete(sessionID), nil
	case "delete-confirmed":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session delete-confirmed <id>"}, nil
		}
		return d.handleSessionDeleteConfirmed(ctx, externalKey, sessionID)
	default:
		return Result{Handled: true, Text: "Usage:\n/session\n/session menu <id>\n/session use <id>\n/session rename <id> [title]\n/session delete <id>\n/session current"}, nil
	}
}

func (d *Dispatcher) handleSessionNew(ctx context.Context, externalKey string, args string) (Result, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return d.sessionRuntimePicker(ctx)
	}
	runtimeValue, title, _ := strings.Cut(args, " ")
	target := parseSessionTarget(runtimeValue)
	if target.runtimeID == "" {
		return Result{Handled: true, Text: "Usage: /session new matrixclaw|AGENT [title]"}, nil
	}
	return d.createSession(ctx, externalKey, target, strings.TrimSpace(title))
}

type sessionTarget struct {
	runtimeID       core.SessionRuntime
	externalAgentID string
}

func (d *Dispatcher) createSession(ctx context.Context, externalKey string, target sessionTarget, title string) (Result, error) {
	runtimeID := core.NormalizeSessionRuntime(target.runtimeID)
	if title = strings.TrimSpace(title); title == "" {
		title = d.defaultSessionTitle(externalKey)
	}
	var session core.Session
	var err error
	if options, ok := d.sessions.(SessionRuntimeOptions); ok {
		request := core.CreateSessionRequest{
			Title:      title,
			RuntimeID:  string(runtimeID),
			WorkingDir: d.workingDir,
		}
		if runtimeID == core.SessionRuntimeExternalAgent {
			request.PermissionMode = string(core.PermissionModeFullAuto)
			request.ExternalAgentID = target.externalAgentID
		}
		session, err = options.CreateSessionWithOptions(ctx, externalKey, request)
	} else if runtimeID == core.SessionRuntimeMatrixClaw {
		session, err = d.sessions.CreateSession(ctx, externalKey, title, d.workingDir)
	} else {
		return Result{Handled: true, Text: "Session runtime " + string(runtimeID) + " is not supported by this client."}, nil
	}
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled:        true,
		Text:           "Current session: " + formatSessionLabel(session, true),
		ReloadSnapshot: true,
	}, nil
}

func parseSessionTarget(value string) sessionTarget {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "matrixclaw", "matrix", "default", "assistant", "core":
		return sessionTarget{runtimeID: core.SessionRuntimeMatrixClaw}
	case "":
		return sessionTarget{}
	default:
		return sessionTarget{runtimeID: core.SessionRuntimeExternalAgent, externalAgentID: value}
	}
}

func (d *Dispatcher) handleSessionMenu(ctx context.Context, externalKey string, sessionID string) (Result, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Picker: d.sessionMenuPicker(session)}, nil
}

func (d *Dispatcher) sessionMenuPicker(session core.Session) *PickerData {
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = session.ID
	}
	picker := NewPickerData(PickerSessionActions, "Session: "+title).
		Context(session.ID).
		Back(sessionsCommand()).
		Row("use", "Use", "Make active", sessionUseCommand(session.ID))
	if d.sessionModels != nil {
		picker.Row("model", "Model", sessionModelInfo(session), sessionModelCommand(session.ID))
	}
	picker.Row("rename", "Rename", title, sessionRenameCommand(session.ID)).
		Danger("delete", "Delete", "Permanent", sessionDeleteCommand(session.ID))
	return picker.Ptr()
}

func sessionModelInfo(session core.Session) string {
	if model := strings.TrimSpace(session.ModelID); model != "" {
		return model
	}
	return "Choose model"
}

func sessionListInfo(session core.Session) string {
	parts := []string{sessionRuntimeLabel(session)}
	if provider := strings.TrimSpace(session.ProviderID); provider != "" {
		parts = append(parts, provider)
	}
	if model := strings.TrimSpace(session.ModelID); model != "" {
		parts = append(parts, model)
	}
	return strings.Join(parts, " · ")
}

func sessionRuntimeLabel(session core.Session) string {
	switch core.NormalizeSessionRuntime(session.RuntimeID) {
	case core.SessionRuntimeExternalAgent:
		if name := strings.TrimSpace(session.ExternalAgentName); name != "" {
			return name
		}
		if id := strings.TrimSpace(session.ExternalAgentID); id != "" {
			return id
		}
		return "External Agent"
	default:
		return "MatrixClaw"
	}
}

func (d *Dispatcher) handleSessionRename(ctx context.Context, sessionID string, title string) (Result, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Result{
			Handled: true,
			Prompt: &PromptData{
				Title:               "Rename Session",
				Placeholder:         "New session title",
				Value:               strings.TrimSpace(session.Title),
				SubmitCommandPrefix: sessionRenameCommandPrefix(session.ID),
				CancelCommand:       sessionMenuCommand(session.ID),
			},
		}, nil
	}
	renamed, err := d.sessions.RenameSession(ctx, session.ID, title)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled:        true,
		Text:           "Renamed session to " + formatSessionLabel(renamed, false),
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) handleSessionModel(ctx context.Context, sessionID string) (Result, error) {
	if d.sessionModels == nil {
		return unsupportedRuntime("session models"), nil
	}
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	response, err := d.sessionModels.SessionModels(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	current := strings.TrimSpace(response.ModelID)
	picker := NewPickerData(PickerSessionModels, "Model").
		Meta(current).
		Context(session.ID).
		Popup()
	for _, modelID := range response.Models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		picker.Item(PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  sessionSetModelCommand(session.ID, modelID),
		})
	}
	if len(picker.Ptr().Items) == 0 {
		return Result{Handled: true, Text: "No models are available for this session."}, nil
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) handleSessionSetModel(ctx context.Context, sessionID string, modelID string) (Result, error) {
	if d.sessionModels == nil {
		return unsupportedRuntime("session models"), nil
	}
	session, err := d.sessionModels.UpdateSessionModel(ctx, sessionID, modelID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled:        true,
		ReloadSnapshot: true,
		Picker:         d.sessionMenuPicker(session),
	}, nil
}

func (d *Dispatcher) handleSessionDelete(sessionID string) Result {
	sessionID = strings.TrimSpace(sessionID)
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("This removes the session history permanently.", sessionDeleteConfirmedCommand(sessionID), sessionMenuCommand(sessionID)),
	}
}

func (d *Dispatcher) handleSessionDeleteConfirmed(ctx context.Context, externalKey string, sessionID string) (Result, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	currentSessionID, _, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if err := d.sessions.DeleteSession(ctx, session.ID); err != nil {
		return Result{}, err
	}

	text := "Deleted session " + formatSessionLabel(session, false) + "."
	if strings.TrimSpace(currentSessionID) == strings.TrimSpace(session.ID) {
		nextText, err := d.rebindAfterDelete(ctx, externalKey)
		if err != nil {
			return Result{}, err
		}
		if nextText != "" {
			text += "\n" + nextText
		}
	}
	return Result{
		Handled:        true,
		Text:           text,
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) handleCurrent(ctx context.Context, externalKey string) (Result, error) {
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "No active session. Use /new or /sessions."}, nil
	}
	lines := []string{
		"Current session: " + formatSessionLabel(*session, true),
		"Runtime: " + sessionRuntimeLabel(*session),
	}
	if strings.TrimSpace(session.ProviderID) != "" {
		lines = append(lines, "Provider: "+strings.TrimSpace(session.ProviderID))
	}
	if strings.TrimSpace(session.ModelID) != "" {
		lines = append(lines, "Model: "+strings.TrimSpace(session.ModelID))
	}
	return Result{
		Handled: true,
		Text:    strings.Join(lines, "\n"),
	}, nil
}

func (d *Dispatcher) currentSession(ctx context.Context, externalKey string) (string, *core.Session, error) {
	if d.sessions == nil {
		return "", nil, nil
	}
	binding, err := d.sessions.CurrentBinding(ctx, externalKey)
	if err != nil {
		return "", nil, err
	}
	sessions, err := d.sessions.ListSessions(ctx)
	if err != nil {
		return "", nil, err
	}
	for i := range sessions {
		if strings.TrimSpace(sessions[i].ID) == strings.TrimSpace(binding.SessionID) {
			return binding.SessionID, &sessions[i], nil
		}
	}
	return binding.SessionID, nil, nil
}

func (d *Dispatcher) findSession(ctx context.Context, sessionID string) (core.Session, error) {
	if d.sessions == nil {
		return core.Session{}, core.ErrNotFound
	}
	sessions, err := d.sessions.ListSessions(ctx)
	if err != nil {
		return core.Session{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	for _, session := range sessions {
		if strings.TrimSpace(session.ID) == sessionID {
			return session, nil
		}
	}
	return core.Session{}, core.ErrNotFound
}

func (d *Dispatcher) rebindAfterDelete(ctx context.Context, externalKey string) (string, error) {
	if d.sessions == nil {
		return "", nil
	}
	sessions, err := d.sessions.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	if len(sessions) > 0 {
		if _, err := d.sessions.UseSession(ctx, externalKey, sessions[0].ID); err != nil {
			return "", err
		}
		return "Current session: " + formatSessionLabel(sessions[0], true), nil
	}
	result, err := d.createSession(ctx, externalKey, sessionTarget{runtimeID: core.SessionRuntimeMatrixClaw}, d.initialSessionTitle(externalKey))
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (d *Dispatcher) defaultSessionTitle(_ string) string {
	return "New chat"
}

func (d *Dispatcher) initialSessionTitle(_ string) string {
	return "Main"
}

func formatSessionLabel(session core.Session, current bool) string {
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = session.ID
	}
	if current {
		return "• " + title + " [" + session.ID + "]"
	}
	return title + " [" + session.ID + "]"
}
