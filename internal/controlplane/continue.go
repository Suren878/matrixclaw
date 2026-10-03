package controlplane

import "context"

func (d *Dispatcher) handleContinue(ctx context.Context) (Result, error) {
	sessionID, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if sessionID == "" {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if session != nil && !d.mayUse(*session) {
		return Result{Handled: true, Text: unattendedRefusal}, nil
	}
	if _, err := d.daemon.ContinueSession(ctx, sessionID, d.workingDir); err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Continuing the last run."}, nil
}
