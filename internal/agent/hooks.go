package agent

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// stopHookLimit is how many replies of a run Stop hooks may hold back.
const stopHookLimit = 5

// stopHookNote asks the run's Stop hooks whether it may end with reply and
// returns their reason to go on; past stopHookLimit they are not asked.
func (r *run) stopHookNote(ctx context.Context, reply string) string {
	if r.Hooks == nil || r.state.StopHooks >= stopHookLimit {
		return ""
	}
	reason := r.Hooks.Stop(ctx, reply, r.state.StopHooks > 0)
	if reason == "" {
		return ""
	}
	r.state.StopHooks++
	return "A Stop hook does not let this run end yet:\n" + reason + "\nContinue the work it points to, then reply again."
}

// sessionStart journals what the SessionStart hooks of a run's first execution
// print: source "startup" for the session's first run, "resume" for later ones.
func (r *run) sessionStart(ctx context.Context, window Window) error {
	if r.Hooks == nil || r.task.Recovering || r.state.Steps > 0 {
		return nil
	}
	source := "startup"
	if window.Boundary != nil {
		source = "resume"
	}
	for _, message := range window.Messages {
		if message.RunID != r.task.RunID {
			source = "resume"
			break
		}
	}
	text := r.Hooks.SessionStart(ctx, source)
	if text == "" {
		return nil
	}
	return r.appendEngineMessage(ctx, transcript.OriginEngineModel, "SessionStart hook context:\n"+text)
}
