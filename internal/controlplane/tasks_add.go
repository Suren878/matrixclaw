package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (d *Dispatcher) handleTaskAdd(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	args = strings.TrimSpace(args)
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return Result{Handled: true, Text: "Usage: /tasks add once 2026-04-26 16:00 -- prompt"}, nil
	}
	binding, err := d.sessions.CurrentBinding(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(binding.SessionID) == "" {
		return Result{Handled: true, Text: "Choose a session or create a new one before creating scheduled tasks."}, nil
	}
	mode := strings.ToLower(fields[0])
	switch mode {
	case "once":
		runAt, prompt, err := parseReminderArgs(d.now(), strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
		if err != nil {
			return Result{Handled: true, Text: err.Error()}, nil
		}
		job, err := d.automation.CreateAutomationJob(ctx, automation.CreateJobInput{
			Kind:         automation.JobKindAITask,
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
		return Result{Handled: true, Text: "Scheduled task created: " + formatAutomationJob(job)}, nil
	case "cron":
		rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		cronExpr, prompt, ok := splitCronAndPrompt(rest)
		if !ok {
			return Result{Handled: true, Text: "Usage: /tasks add cron \"0 10 1 * *\" -- prompt"}, nil
		}
		job, err := d.automation.CreateAutomationJob(ctx, automation.CreateJobInput{
			Kind:         automation.JobKindAITask,
			SessionID:    binding.SessionID,
			Client:       d.automation.ClientName(),
			ExternalKey:  externalKey,
			ScheduleMode: automation.ScheduleModeCron,
			CronExpr:     cronExpr,
			Prompt:       prompt,
		})
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Scheduled task created: " + formatAutomationJob(job)}, nil
	default:
		return Result{Handled: true, Text: "Usage: /tasks add once <time> -- prompt or /tasks add cron \"<expr>\" -- prompt"}, nil
	}
}
