package runtime

import (
	"context"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (r *Runtime) loadSnapshot(ctx context.Context) (core.ClientSnapshot, error) {
	client, err := r.daemon()
	if err != nil {
		return core.ClientSnapshot{}, err
	}
	return client.LoadSnapshot(ctx)
}

func (r *Runtime) ensureSession(ctx context.Context) (string, error) {
	client, err := r.daemon()
	if err != nil {
		return "", err
	}

	binding, err := client.CurrentBinding(ctx)
	if err == nil && strings.TrimSpace(binding.SessionID) != "" {
		return binding.SessionID, nil
	}
	if err != nil && !daemonclient.IsAPIStatus(err, http.StatusNotFound) {
		return "", err
	}

	sessions, err := client.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		session, err := client.CreateSession(ctx, defaultInitialSessionTitle(), r.config.WorkingDir)
		if err != nil {
			return "", err
		}
		sessions = append(sessions, session)
	}

	binding, err = client.UseSession(ctx, sessions[0].ID)
	if err != nil {
		return "", err
	}
	return binding.SessionID, nil
}

func (r *Runtime) loadOrInitSnapshot(ctx context.Context) (core.ClientSnapshot, error) {
	if _, err := r.ensureSession(ctx); err != nil {
		return core.ClientSnapshot{}, err
	}
	return r.loadSnapshot(ctx)
}

func (r *Runtime) createAndLoadSession(ctx context.Context, title string) (core.ClientSnapshot, error) {
	client, err := r.daemon()
	if err != nil {
		return core.ClientSnapshot{}, err
	}
	session, err := client.CreateSession(ctx, title, r.config.WorkingDir)
	if err != nil {
		return core.ClientSnapshot{}, err
	}
	if _, err := client.UseSession(ctx, session.ID); err != nil {
		return core.ClientSnapshot{}, err
	}
	return client.LoadSnapshot(ctx)
}

func defaultInitialSessionTitle() string {
	return "Main"
}

func defaultNewSessionTitle() string {
	return "New chat"
}
