package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (d *Dispatcher) handleRemind(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.automation == nil {
		return unsupportedRuntime("reminders"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return Result{Handled: true, Text: "Usage:\n/remind in 10m -- message\n/remind 2026-04-26 16:00 -- message"}, nil
	}
	runAt, prompt, err := parseReminderArgs(d.now(), args)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	binding, err := d.sessions.CurrentBinding(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(binding.SessionID) == "" {
		return Result{Handled: true, Text: "Choose a session or create a new one before creating reminders."}, nil
	}
	job, err := d.automation.CreateAutomationJob(ctx, automation.CreateJobInput{
		Kind:         automation.JobKindReminder,
		SessionID:    binding.SessionID,
		Client:       d.automation.ClientName(),
		ExternalKey:  externalKey,
		ScheduleMode: automation.ScheduleModeOnce,
		RunAt:        &runAt,
		Prompt:       prompt,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "⏰ Reminder scheduled: " + formatAutomationJob(job)}, nil
}
