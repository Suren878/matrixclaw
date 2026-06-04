package controlplane

import (
	"fmt"
	"strings"
	"time"
)

func parseReminderArgs(now time.Time, args string) (time.Time, string, error) {
	whenPart, prompt, ok := strings.Cut(strings.TrimSpace(args), "--")
	if !ok || strings.TrimSpace(prompt) == "" {
		return time.Time{}, "", fmt.Errorf("usage: /remind in 10m -- message")
	}
	whenPart = strings.TrimSpace(whenPart)
	prompt = strings.TrimSpace(prompt)
	if strings.HasPrefix(strings.ToLower(whenPart), "in ") {
		duration, err := parseHumanDuration(strings.TrimSpace(whenPart[3:]))
		if err != nil {
			return time.Time{}, "", fmt.Errorf("invalid duration: use 10m, 2h, or 3d")
		}
		return now.Add(duration).UTC(), prompt, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, whenPart, time.Local); err == nil {
			return parsed.UTC(), prompt, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("invalid time: use YYYY-MM-DD HH:MM -- message")
}

func parseHumanDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasSuffix(value, "d") {
		days, err := time.ParseDuration(strings.TrimSuffix(value, "d") + "h")
		if err != nil {
			return 0, err
		}
		return days * 24, nil
	}
	return time.ParseDuration(value)
}

func splitCronAndPrompt(value string) (string, string, bool) {
	left, prompt, ok := strings.Cut(strings.TrimSpace(value), "--")
	if !ok {
		return "", "", false
	}
	left = strings.TrimSpace(left)
	prompt = strings.TrimSpace(prompt)
	if strings.HasPrefix(left, "\"") && strings.Contains(left[1:], "\"") {
		end := strings.Index(left[1:], "\"") + 1
		left = left[1:end]
	}
	if left == "" || prompt == "" {
		return "", "", false
	}
	return left, prompt, true
}
