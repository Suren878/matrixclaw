package automation

import (
	"fmt"
	"strings"
	"time"
)

func renderPrompt(job Job, scheduledFor time.Time, now time.Time) string {
	title := strings.TrimSpace(job.Title)
	scheduled := formatTimeInLocation(scheduledFor, job.Timezone)
	prompt := strings.TrimSpace(job.Prompt)
	if job.Kind == JobKindReminder {
		return fmt.Sprintf("⏰ Reminder: %s\n🕒 %s\n📝 %s", title, scheduled, prompt)
	}

	return fmt.Sprintf("🗓 Scheduled Task: %s\n🕒 Scheduled: %s\n🕘 Now: %s\n🧭 Instruction:\n%s",
		title,
		scheduled,
		formatTimeInLocation(now, job.Timezone),
		prompt,
	)
}

func formatTimeInLocation(value time.Time, timezone string) string {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		loc = time.UTC
	}
	return value.In(loc).Format("2006-01-02 15:04 MST")
}

func titleFromPrompt(prompt string) string {
	prompt = strings.Join(strings.Fields(prompt), " ")
	if len(prompt) > 48 {
		return strings.TrimSpace(prompt[:48]) + "..."
	}
	return prompt
}
