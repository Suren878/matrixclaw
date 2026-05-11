package controlplane

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func formatAutomationJob(job automation.Job) string {
	next := "not scheduled"
	if job.NextDueAt != nil {
		next = job.NextDueAt.Format("2006-01-02 15:04 UTC")
	}
	return fmt.Sprintf("%s [%s] %s - next: %s", job.ID, job.Status, job.Title, next)
}

func splitAutomationJobs(jobs []automation.Job) ([]automation.Job, []automation.Job) {
	active := make([]automation.Job, 0, len(jobs))
	closed := make([]automation.Job, 0, len(jobs))
	for _, job := range jobs {
		if job.Status == automation.JobStatusActive {
			active = append(active, job)
			continue
		}
		if job.Status != automation.JobStatusDeleted {
			closed = append(closed, job)
		}
	}
	return active, closed
}

func findAutomationJob(jobs []automation.Job, jobID string) (automation.Job, bool) {
	jobID = strings.TrimSpace(jobID)
	for _, job := range jobs {
		if strings.EqualFold(strings.TrimSpace(job.ID), jobID) {
			return job, true
		}
	}
	return automation.Job{}, false
}

func taskListTitle(job automation.Job) string {
	title := firstTaskText(job.Title, job.Prompt, job.ID)
	return truncateTaskText(title, 64)
}

func taskListInfo(job automation.Job) string {
	switch job.Status {
	case automation.JobStatusActive:
		return nextAutomationLabel(job)
	default:
		return string(job.Status)
	}
}

func nextAutomationLabel(job automation.Job) string {
	if job.NextDueAt == nil {
		return "not scheduled"
	}
	return "next " + job.NextDueAt.Format("2006-01-02 15:04")
}

func truncateTaskText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func firstTaskText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
