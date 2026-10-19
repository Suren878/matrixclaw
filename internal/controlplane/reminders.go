package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

// The time and message are the value of /remind.
var _ = valueVerb(CommandRemind, "", 0)

func (d *Dispatcher) handleRemind(ctx context.Context, r Route) (Result, error) {
	args := r.Value
	if args == "" {
		return textResult("Usage:\n/remind in 10m -- message\n/remind YYYY-MM-DD HH:MM -- message"), nil
	}
	runAt, prompt, err := parseReminderArgs(d.now(), args)
	if err != nil {
		return textResult(err.Error()), nil
	}
	binding, err := d.currentBinding(ctx)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(binding.SessionID) == "" {
		return textResult("Choose a session or create a new one before creating reminders."), nil
	}
	job, err := d.daemon.CreateAutomationJob(ctx, automation.CreateJobInput{
		Kind:               automation.JobKindReminder,
		SessionID:          binding.SessionID,
		Client:             d.daemon.ClientName,
		ExternalKey:        d.daemon.ExternalKey,
		ClientCapabilities: d.daemon.Capabilities,
		ScheduleMode:       automation.ScheduleModeOnce,
		RunAt:              &runAt,
		Prompt:             prompt,
	})
	if err != nil {
		return Result{}, err
	}
	return textResult("⏰ Reminder scheduled: " + formatAutomationJob(job)), nil
}
