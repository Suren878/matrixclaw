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

var (
	newSessionRoot       = valueVerb(CommandNewSession, "", 0)
	sessionsRoot         = verb(CommandSessions, "", 0)
	sessionNew           = valueVerb(CommandSession, "new", 1)
	sessionMenu          = verb(CommandSession, "menu", 1)
	sessionUse           = verb(CommandSession, "use", 1)
	sessionCurrent       = verb(CommandSession, "current", 0)
	sessionRename        = valueVerb(CommandSession, "rename", 1)
	sessionModel         = verb(CommandSession, "model", 1)
	sessionSetModel      = verb(CommandSession, "set-model", 2)
	sessionDelete        = verb(CommandSession, "delete", 1)
	sessionDeleteConfirm = verb(CommandSession, "delete-confirmed", 1)
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
	picker := NewPickerData(PickerSessions, "Sessions").Command(sessionsRoot.command())
	picker.Item(PickerItem{
		ID:      "new",
		Title:   "New Session",
		Command: newSessionRoot.command(),
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
			Command:  sessionMenu.command(session.ID),
		})
	}
	return Result{
		Screen: picker.Build(),
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

func (d *Dispatcher) handleNewSession(ctx context.Context, r Route) (Result, error) {
	if r.Value == "" {
		return d.sessionRuntimePicker(ctx)
	}
	return d.createSession(ctx, "", r.Value)
}

func (d *Dispatcher) sessionRuntimePicker(ctx context.Context) (Result, error) {
	picker := NewPickerData(PickerSessionRuntime, "New Session").
		Back(sessionsRoot.command()).
		Row("matrixclaw", "Matrixclaw", "Built-in", sessionNew.command("matrixclaw"))
	if d.owner() {
		agents, err := d.daemon.ListExternalAgents(ctx)
		if err != nil {
			return Result{}, err
		}
		for _, agent := range agents {
			if !agent.Installed || !agent.Enabled {
				continue
			}
			picker.Row(agent.ID, externalAgentTitle(agent), externalAgentSessionRuntimeInfo(agent), sessionNew.command(agent.ID))
		}
	}
	return Result{
		Screen: picker.Build(),
	}, nil
}

func externalAgentSessionRuntimeInfo(agent core.ExternalAgentDescriptor) string {
	return "External"
}

func (d *Dispatcher) handleSession(ctx context.Context, r Route) (Result, error) {
	sessionID := r.arg(0)
	switch r.Verb {
	case sessionCurrent:
		return d.handleCurrent(ctx)
	case sessionNew:
		agentID, ok := parseSessionTarget(sessionID)
		if !ok {
			return d.sessionRuntimePicker(ctx)
		}
		return d.createSession(ctx, agentID, r.Value)
	}
	if sessionID == "" {
		if r.Value != "" {
			return textResult("Usage:\n/session\n/session menu <id>\n/session use <id>\n/session rename <id> [title]\n/session delete <id>\n/session current"), nil
		}
		return d.handleSessions(ctx)
	}
	switch r.Verb {
	case sessionMenu:
		return d.handleSessionMenu(ctx, sessionID)
	case sessionUse:
		if target, err := d.findSession(ctx, sessionID); err == nil && !d.mayUse(target) {
			return textResult(unattendedRefusal), nil
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
		return Result{Screen: TextData{Text: text}, ReloadSnapshot: true}, nil
	case sessionRename:
		return d.handleSessionRename(ctx, sessionID, r.Value)
	case sessionModel:
		return d.handleSessionModel(ctx, sessionID)
	case sessionSetModel:
		if r.arg(1) == "" {
			return textResult("Usage: /session set-model <id> <model>"), nil
		}
		return d.handleSessionSetModel(ctx, sessionID, r.arg(1))
	case sessionDelete:
		return d.handleSessionDelete(sessionID), nil
	case sessionDeleteConfirm:
		return d.handleSessionDeleteConfirmed(ctx, sessionID)
	}
	return d.handleSessions(ctx)
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

// createSession starts a session run by the external agent agentID, or a
// native one when agentID is empty, and makes it current.
func (d *Dispatcher) createSession(ctx context.Context, agentID string, title string) (Result, error) {
	if agentID != "" && !d.owner() {
		return textResult("Only the owner can start an external agent session."), nil
	}
	if title = strings.TrimSpace(title); title == "" {
		title = d.defaultSessionTitle()
	}
	request := core.CreateSessionRequest{
		Title:      title,
		WorkingDir: d.workingDir,
	}
	if agentID != "" {
		request.Kind = string(core.SessionKindExternalAgent)
		request.PermissionMode = string(core.PermissionModeFullAuto)
		request.ExternalAgentID = agentID
	}
	session, err := d.daemon.CreateSessionWithRequest(ctx, request)
	if err == nil {
		_, err = d.daemon.UseSession(ctx, session.ID)
	}
	if err != nil {
		return Result{}, err
	}
	return Result{
		Screen:         TextData{Text: "Current session: " + formatSessionLabel(session, true)},
		ReloadSnapshot: true,
	}, nil
}

// parseSessionTarget is the external agent a /session new names: none for
// the built-in runtime; false when it names nothing.
func parseSessionTarget(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "matrixclaw", "matrix", "default", "assistant", "core":
		return "", true
	case "":
		return "", false
	default:
		return value, true
	}
}

func (d *Dispatcher) handleSessionMenu(ctx context.Context, sessionID string) (Result, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: d.sessionMenuPicker(session)}, nil
}

func (d *Dispatcher) sessionMenuPicker(session core.Session) PickerData {
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = session.ID
	}
	picker := NewPickerData(PickerSessionActions, "Session: "+title).
		Back(sessionsRoot.command()).
		Row("use", "Use", "Make active", sessionUse.command(session.ID)).
		Row("model", "Model", sessionModelInfo(session), sessionModel.command(session.ID)).
		Row("rename", "Rename", title, sessionRename.command(session.ID)).
		Danger("delete", "Delete", "Permanent", sessionDelete.command(session.ID))
	return picker.Build()
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
	switch session.Kind {
	case core.SessionKindExternalAgent:
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
			Screen: PromptData{
				Title:         "Rename Session",
				Placeholder:   "New session title",
				Value:         strings.TrimSpace(session.Title),
				Submit:        sessionRename.command(session.ID),
				CancelCommand: sessionMenu.command(session.ID),
			},
		}, nil
	}
	renamed, err := d.daemon.RenameSession(ctx, session.ID, title)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Screen:         TextData{Text: "Renamed session to " + formatSessionLabel(renamed, false)},
		ReloadSnapshot: true,
	}, nil
}

// handleSessionModel opens the provider list of a MatrixClaw session and the
// model list of an external agent session.
func (d *Dispatcher) handleSessionModel(ctx context.Context, sessionID string) (Result, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	if !core.CoreSessionIsExternalAgent(session) {
		return d.sessionProviderPicker(ctx, session, sessionModel.command(session.ID), sessionMenu.command(session.ID))
	}
	response, err := d.daemon.SessionModels(ctx, session.ID, "")
	if err != nil {
		return Result{}, err
	}
	if len(response.Models) == 0 {
		return textResult("No models are available for this session."), nil
	}
	choice := modelChoice{
		title:   "Model",
		command: sessionModel.command(session.ID),
		back:    sessionMenu.command(session.ID),
		current: response.ModelID,
		models:  response.Models,
		choose:  func(model string) string { return sessionSetModel.command(session.ID, model) },
	}
	return Result{Screen: choice.picker()}, nil
}

func (d *Dispatcher) handleSessionSetModel(ctx context.Context, sessionID string, modelID string) (Result, error) {
	session, err := d.daemon.UpdateSessionLLM(ctx, sessionID, "", modelID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Screen: d.sessionMenuPicker(session), ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) handleSessionDelete(sessionID string) Result {
	sessionID = strings.TrimSpace(sessionID)
	return Result{
		Screen: deleteConfirmData("This removes the session history permanently.", sessionDeleteConfirm.command(sessionID), sessionMenu.command(sessionID)),
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
		Screen:         TextData{Text: text},
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) handleCurrent(ctx context.Context) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return textResult("No active session. Use /new or /sessions."), nil
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
		Screen: TextData{Text: strings.Join(lines, "\n")},
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
	result, err := d.createSession(ctx, "", d.initialSessionTitle())
	if err != nil {
		return "", err
	}
	return result.Text(), nil
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
