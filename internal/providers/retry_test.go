package providers

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterPreservesServerDeadline(t *testing.T) {
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"0", 0}, {"2", 2 * time.Second}, {"120", 2 * time.Minute},
		{now.Add(5 * time.Second).Format(http.TimeFormat), 5 * time.Second},
		{"invalid", time.Second}, {"-1", time.Second},
	} {
		if got := RetryAfterDelay(tc.header, now, time.Second); got != tc.want {
			t.Errorf("Retry-After %q = %s, want %s", tc.header, got, tc.want)
		}
	}
}
