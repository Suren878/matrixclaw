package store

import (
	"strconv"
	"strings"
	"time"
)

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func mustParseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		if parsed, err = time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
		if unix, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
			return time.Unix(0, unix).UTC()
		}
		return time.Time{}
	}
	return parsed
}
