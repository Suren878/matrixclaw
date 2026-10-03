package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func (d *Dispatcher) handleSessions(ctx context.Context) (Result, error) {
	currentSessionID, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	sessions, err := d.daemon.ListSessions(ctx)
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

func (d *Dispatcher) handleNewSession(ctx context.Context, args string) (Result, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return d.sessionRuntimePicker(ctx)
	}
	return d.createSession(ctx, sessionTarget{runtimeID: core.SessionRuntimeMatrixClaw}, args)
}

func (d *Dispatcher) sessionRuntimePicker(ctx context.Context) (Result, error) {
	picker := NewPickerData(PickerSessionRuntime, "New Session").
		Back(sessionsCommand()).
		Row("matrixclaw", "Matrixclaw", "Built-in", sessionNewCommand("matrixclaw"))
	if d.owner() {
		agents, err := d.daemon.ListExternalAgents(ctx)
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

func (d *Dispatcher) handleSession(ctx context.Context, args string) (Result, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return d.handleSessions(ctx)
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "new":
		return d.handleSessionNew(ctx, rest)
	case "menu":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session menu <id>"}, nil
		}
		return d.handleSessionMenu(ctx, sessionID)
	case "use":
		sessionID, _ := firstCommandToken(rest)
		if sessionID == "" {
			return Result{Handled: true, Text: "Usage: /session use <id>"}, nil
		}
		if target, err := d.findSession(ctx, sessionID); err == nil && !d.mayUse(target) {
			return Result{Handled: true, Text: unattendedRefusal}, nil
		}
		binding, err := d.daemon.UseSession(ctx, sessionID)
		if err != nil {
			return Result{}, err
		}
		_, session, err := d.currentSession(ctx)
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
		return d.handleCurrent(ctx)
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
		return d.handleSessionDeleteConfirmed(ctx, sessionID)
	default:
		return Result{Handled: true, Text: "Usage:\n/session\n/session menu <id>\n/session use <id>\n/session rename <id> [title]\n/session delete <id>\n/session current"}, nil
	}
}

func (d *Dispatcher) handleSessionNew(ctx context.Context, args string) (Result, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return d.sessionRuntimePicker(ctx)
	}
	runtimeValue, title, _ := strings.Cut(args, " ")
	target := parseSessionTarget(runtimeValue)
	if target.runtimeID == "" {
		return Result{Handled: true, Text: "Usage: /session new matrixclaw|AGENT [title]"}, nil
	}
	return d.createSession(ctx, target, strings.TrimSpace(title))
}

type sessionTarget struct {
	runtimeID       core.SessionRuntime
	externalAgentID string
}

// owner reports whether this client acts for the owner: the only one that may
// change settings and modes, start external agent sessions (they run in
// full_auto) and use any session that core.RunsUnattended.
func (d *Dispatcher) owner() bool {
	return d.daemon.Role == "" || d.daemon.Role == core.RoleOwner
}

// managesRules reports whether this client may add and delete rules of scope;
// the daemon refuses the others.
func (d *Dispatcher) managesRules(scope permission.Scope) bool {
	switch d.daemon.Role {
	case core.RoleGuest:
		return false
	case core.RoleMember:
		return scope != permission.ScopeGlobal
	default:
		return true
	}
}

// mayUse reports whether this client may bind to or send into session.
func (d *Dispatcher) mayUse(session core.Session) bool {
	return d.owner() || !core.RunsUnattended(session)
}

const ownerOnlySettings = "Only the owner can change settings."

const unattendedRefusal = "Only the owner can use a session that runs tools without asking (external agent or full_auto)."

func (d *Dispatcher) createSession(ctx context.Context, target sessionTarget, title string) (Result, error) {
	runtimeID := core.NormalizeSessionRuntime(target.runtimeID)
	if runtimeID == core.SessionRuntimeExternalAgent && !d.owner() {
		return Result{Handled: true, Text: "Only the owner can start an external agent session."}, nil
	}
	if title = strings.TrimSpace(title); title == "" {
		title = d.defaultSessionTitle()
	}
	request := core.CreateSessionRequest{
		Title:      title,
		RuntimeID:  string(runtimeID),
		WorkingDir: d.workingDir,
	}
	if runtimeID == core.SessionRuntimeExternalAgent {
		request.PermissionMode = string(core.PermissionModeFullAuto)
		request.ExternalAgentID = target.externalAgentID
	}
	session, err := d.daemon.CreateSessionWithRequest(ctx, request)
	if err == nil {
		_, err = d.daemon.UseSession(ctx, session.ID)
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

func (d *Dispatcher) handleSessionMenu(ctx context.Context, sessionID string) (Result, error) {
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
	picker.Row("model", "Model", sessionModelInfo(session), sessionModelCommand(session.ID))
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
	renamed, err := d.daemon.RenameSession(ctx, session.ID, title)
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
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	response, err := d.daemon.SessionModels(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	picker := sessionModelPicker(session.ID, response)
	if len(picker.Items) == 0 {
		return Result{Handled: true, Text: "No models are available for this session."}, nil
	}
	return Result{Handled: true, Picker: picker}, nil
}

func sessionModelPicker(sessionID string, response core.SessionModelsResponse) *PickerData {
	current := strings.TrimSpace(response.ModelID)
	picker := NewPickerData(PickerSessionModels, "Model").
		Meta(current).
		Context(sessionID).
		Select(sessionMenuCommand(sessionID))
	for _, modelID := range response.Models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		picker.Item(PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  sessionSetModelCommand(sessionID, modelID),
		})
	}
	return picker.Ptr()
}

func (d *Dispatcher) handleSessionSetModel(ctx context.Context, sessionID string, modelID string) (Result, error) {
	session, err := d.daemon.UpdateSessionModel(ctx, sessionID, modelID)
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

func (d *Dispatcher) handleSessionDeleteConfirmed(ctx context.Context, sessionID string) (Result, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	currentSessionID, _, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := d.daemon.DeleteSession(ctx, session.ID); err != nil {
		return Result{}, err
	}

	text := "Deleted session " + formatSessionLabel(session, false) + "."
	if strings.TrimSpace(currentSessionID) == strings.TrimSpace(session.ID) {
		nextText, err := d.rebindAfterDelete(ctx)
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

func (d *Dispatcher) handleCurrent(ctx context.Context) (Result, error) {
	_, session, err := d.currentSession(ctx)
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

func (d *Dispatcher) currentSession(ctx context.Context) (string, *core.Session, error) {
	binding, err := d.currentBinding(ctx)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(binding.SessionID) == "" {
		return binding.SessionID, nil, nil
	}
	session, err := d.findSession(ctx, binding.SessionID)
	if errors.Is(err, core.ErrNotFound) {
		return binding.SessionID, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return binding.SessionID, &session, nil
}

func (d *Dispatcher) findSession(ctx context.Context, sessionID string) (core.Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return core.Session{}, core.ErrNotFound
	}
	session, err := d.daemon.GetSession(ctx, sessionID)
	if daemonclient.IsAPIStatus(err, http.StatusNotFound) {
		return core.Session{}, core.ErrNotFound
	}
	return session, err
}

// currentBinding is the session this client is bound to; none is no error.
func (d *Dispatcher) currentBinding(ctx context.Context) (core.ClientBinding, error) {
	binding, err := d.daemon.CurrentBinding(ctx)
	if daemonclient.IsAPIStatus(err, http.StatusNotFound) {
		return core.ClientBinding{}, nil
	}
	return binding, err
}

func (d *Dispatcher) rebindAfterDelete(ctx context.Context) (string, error) {
	sessions, err := d.daemon.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	for _, session := range sessions {
		if !d.mayUse(session) {
			continue
		}
		if _, err := d.daemon.UseSession(ctx, session.ID); err != nil {
			return "", err
		}
		return "Current session: " + formatSessionLabel(session, true), nil
	}
	result, err := d.createSession(ctx, sessionTarget{runtimeID: core.SessionRuntimeMatrixClaw}, d.initialSessionTitle())
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (d *Dispatcher) defaultSessionTitle() string {
	return "New chat"
}

func (d *Dispatcher) initialSessionTitle() string {
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
