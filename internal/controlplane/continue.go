package controlplane

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleContinue(ctx context.Context) (Result, error) {
	sessionID, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if sessionID == "" {
		return textResult("Select or create a session first."), nil
	}
	if session != nil && !d.mayUse(*session) {
		return textResult(unattendedRefusal), nil
	}
	if _, err := d.daemon.Send(ctx, core.HandleMessageInput{SessionID: sessionID, WorkingDir: d.workingDir, Continue: true}); err != nil {
		return Result{}, err
	}
	return textResult("Continuing the last run."), nil
}
