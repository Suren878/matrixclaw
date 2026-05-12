package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type ReminderTool struct {
	service *Service
}

type ScheduledAITaskTool struct {
	service *Service
}

func NewReminderTool(service *Service) *ReminderTool {
	return &ReminderTool{service: service}
}

func NewScheduledAITaskTool(service *Service) *ScheduledAITaskTool {
	return &ScheduledAITaskTool{service: service}
}

func (t *ReminderTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "create_reminder",
		Name:        "Create Reminder",
		Description: "Create a one-time reminder. Use only after resolving the exact time and timezone with the user.",
		Risk:        tools.RiskSafe,
		Namespace:   "core.automation",
		Category:    tools.CategoryAutomation,
		Profiles:    []tools.Profile{tools.ProfileAutomation},
		OutputKind:  tools.OutputText,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "run_at": {"type": "string", "description": "RFC3339 timestamp"},
    "text": {"type": "string"},
    "title": {"type": "string"}
  },
  "required": ["run_at", "text"]
}`),
	}
}

func (t *ReminderTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.service == nil {
		return tools.Result{}, fmt.Errorf("%w: automation service not configured", core.ErrExecutionUnavailable)
	}
	var input struct {
		RunAt string `json:"run_at"`
		Text  string `json:"text"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{IsError: true, Content: "Invalid reminder arguments."}, nil
	}
	runAt, err := time.Parse(time.RFC3339, strings.TrimSpace(input.RunAt))
	if err != nil {
		return tools.Result{IsError: true, Content: "run_at must be an RFC3339 timestamp."}, nil
	}
	job, err := t.service.CreateJobForRun(ctx, call.RunID, CreateJobInput{
		Kind:         JobKindReminder,
		SessionID:    call.SessionID,
		Title:        strings.TrimSpace(input.Title),
		ScheduleMode: ScheduleModeOnce,
		RunAt:        &runAt,
		Prompt:       strings.TrimSpace(input.Text),
	})
	if err != nil {
		if errors.Is(err, core.ErrInvalidInput) {
			return tools.Result{IsError: true, Content: err.Error()}, nil
		}
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("Reminder scheduled: %s at %s (%s)", job.Title, formatTimeInLocation(*job.RunAt, job.Timezone), job.ID)}, nil
}

func (t *ScheduledAITaskTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "create_scheduled_ai_task",
		Name:        "Create Scheduled AI Task",
		Description: "Create a scheduled AI task. Use for recurring or one-time tasks that should run later in the current session.",
		Risk:        tools.RiskApproval,
		Namespace:   "core.automation",
		Category:    tools.CategoryAutomation,
		Profiles:    []tools.Profile{tools.ProfileAutomation},
		OutputKind:  tools.OutputText,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "schedule_mode": {"type": "string", "enum": ["once", "cron"]},
    "run_at": {"type": "string", "description": "RFC3339 timestamp for once schedules"},
    "cron_expr": {"type": "string", "description": "Standard 5-field cron expression"},
    "prompt": {"type": "string"},
    "title": {"type": "string"}
  },
  "required": ["schedule_mode", "prompt"]
}`),
	}
}

func (t *ScheduledAITaskTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.service == nil {
		return tools.Result{}, fmt.Errorf("%w: automation service not configured", core.ErrExecutionUnavailable)
	}
	var input scheduledAITaskToolInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{IsError: true, Content: "Invalid scheduled task arguments."}, nil
	}
	create, result := t.validateScheduledAITaskInput(input, call.SessionID)
	if result.IsError {
		return result, nil
	}
	if !call.Approved {
		return tools.Result{
			Approval: &tools.ApprovalRequest{
				ToolID:      "create_scheduled_ai_task",
				Action:      "schedule_ai_task",
				Description: "Create scheduled AI task: " + strings.TrimSpace(input.Title),
				Params:      input,
			},
		}, nil
	}
	job, err := t.service.CreateJobForRun(ctx, call.RunID, create)
	if err != nil {
		if errors.Is(err, core.ErrInvalidInput) {
			return tools.Result{IsError: true, Content: err.Error()}, nil
		}
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("Scheduled AI task created: %s", job.ID)}, nil
}

type scheduledAITaskToolInput struct {
	ScheduleMode string `json:"schedule_mode"`
	RunAt        string `json:"run_at"`
	CronExpr     string `json:"cron_expr"`
	Prompt       string `json:"prompt"`
	Title        string `json:"title"`
}

func (t *ScheduledAITaskTool) validateScheduledAITaskInput(input scheduledAITaskToolInput, sessionID string) (CreateJobInput, tools.Result) {
	create := CreateJobInput{
		Kind:      JobKindAITask,
		SessionID: sessionID,
		Title:     strings.TrimSpace(input.Title),
		Prompt:    strings.TrimSpace(input.Prompt),
	}
	switch ScheduleMode(strings.TrimSpace(input.ScheduleMode)) {
	case ScheduleModeOnce:
		runAt, err := time.Parse(time.RFC3339, strings.TrimSpace(input.RunAt))
		if err != nil {
			return CreateJobInput{}, tools.Result{IsError: true, Content: "run_at must be an RFC3339 timestamp for once schedules."}
		}
		create.ScheduleMode = ScheduleModeOnce
		create.RunAt = &runAt
	case ScheduleModeCron:
		create.ScheduleMode = ScheduleModeCron
		create.CronExpr = strings.TrimSpace(input.CronExpr)
	default:
		return CreateJobInput{}, tools.Result{IsError: true, Content: "schedule_mode must be once or cron."}
	}
	if _, err := t.service.buildJob(create); err != nil {
		if errors.Is(err, core.ErrInvalidInput) {
			return CreateJobInput{}, tools.Result{IsError: true, Content: err.Error()}
		}
		return CreateJobInput{}, tools.Result{IsError: true, Content: err.Error()}
	}
	return create, tools.Result{}
}
