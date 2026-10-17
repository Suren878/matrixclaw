package providers

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// retryAfter is the wait a response asks for: OpenAI's retry-after-ms first,
// then Retry-After in seconds or as a date; 0 when it names none.
func retryAfter(header http.Header, now time.Time) time.Duration {
	if ms, err := strconv.ParseFloat(strings.TrimSpace(header.Get("Retry-After-Ms")), 64); err == nil && ms >= 0 {
		return capDuration(ms * float64(time.Millisecond))
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return capDuration(seconds * float64(time.Second))
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// ParseRetryDelay reads a delay a provider wrote in an error body, such as
// Gemini's "41.5s" or Codex's resets_in_seconds; 0 when it names none.
func ParseRetryDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return capDuration(seconds * float64(time.Second))
	}
	return 0
}

func capDuration(nanos float64) time.Duration {
	if nanos >= float64(1<<63-1) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(nanos)
}
