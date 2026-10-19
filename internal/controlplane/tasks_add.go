package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (d *Dispatcher) handleTaskAdd(ctx context.Context, args string) (Result, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return textResult("Usage: /tasks add once YYYY-MM-DD HH:MM -- prompt"), nil
	}
	binding, err := d.currentBinding(ctx)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(binding.SessionID) == "" {
		return textResult("Choose a session or create a new one before creating scheduled tasks."), nil
	}
	mode := strings.ToLower(fields[0])
	switch mode {
	case "once":
		runAt, prompt, err := parseReminderArgs(d.now(), strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
		if err != nil {
			return textResult(err.Error()), nil
		}
		job, err := d.daemon.CreateAutomationJob(ctx, automation.CreateJobInput{
			Kind:               automation.JobKindAITask,
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
		return textResult("Scheduled task created: " + formatAutomationJob(job)), nil
	case "cron":
		rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		cronExpr, prompt, ok := splitCronAndPrompt(rest)
		if !ok {
			return textResult("Usage: /tasks add cron \"0 10 1 * *\" -- prompt"), nil
		}
		job, err := d.daemon.CreateAutomationJob(ctx, automation.CreateJobInput{
			Kind:               automation.JobKindAITask,
			SessionID:          binding.SessionID,
			Client:             d.daemon.ClientName,
			ExternalKey:        d.daemon.ExternalKey,
			ClientCapabilities: d.daemon.Capabilities,
			ScheduleMode:       automation.ScheduleModeCron,
			CronExpr:           cronExpr,
			Prompt:             prompt,
		})
		if err != nil {
			return Result{}, err
		}
		return textResult("Scheduled task created: " + formatAutomationJob(job)), nil
	default:
		return textResult("Usage: /tasks add once <time> -- prompt or /tasks add cron \"<expr>\" -- prompt"), nil
	}
}
