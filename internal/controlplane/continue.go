package controlplane

import "context"

func (d *Dispatcher) handleContinue(ctx context.Context, externalKey string) (Result, error) {
	if d.continuer == nil {
		return unsupportedRuntime("continue"), nil
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if sessionID == "" {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if _, err := d.continuer.ContinueSession(ctx, externalKey, sessionID); err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Continuing the last run."}, nil
}
