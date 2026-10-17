package providers

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterPrefersMillisecondsThenSecondsThenDate(t *testing.T) {
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header http.Header
		want   time.Duration
	}{
		{http.Header{"Retry-After": {"0"}}, 0},
		{http.Header{"Retry-After": {"2"}}, 2 * time.Second},
		{http.Header{"Retry-After": {"120"}}, 2 * time.Minute},
		{http.Header{"Retry-After": {now.Add(5 * time.Second).Format(http.TimeFormat)}}, 5 * time.Second},
		{http.Header{"Retry-After": {"invalid"}}, 0},
		{http.Header{"Retry-After": {"-1"}}, 0},
		{http.Header{"Retry-After-Ms": {"1500"}, "Retry-After": {"2"}}, 1500 * time.Millisecond},
		{http.Header{"Retry-After-Ms": {"bad"}, "Retry-After": {"2"}}, 2 * time.Second},
		{http.Header{}, 0},
	} {
		if got := retryAfter(tc.header, now); got != tc.want {
			t.Errorf("%v = %s, want %s", tc.header, got, tc.want)
		}
	}
}

func TestParseRetryDelayReadsBodyDelays(t *testing.T) {
	for value, want := range map[string]time.Duration{"41.5s": 41500 * time.Millisecond, "3600": time.Hour, "": 0, "soon": 0} {
		if got := ParseRetryDelay(value); got != want {
			t.Errorf("%q = %s, want %s", value, got, want)
		}
	}
}
