package providers

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryAfterDelay supports both HTTP forms. Callers may decline a retry whose
// delay exceeds their budget, but must not retry before the server's deadline.
func RetryAfterDelay(header string, now time.Time, fallback time.Duration) time.Duration {
	delay := fallback
	header = strings.TrimSpace(header)
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil && seconds >= 0 {
		if seconds > int64((1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		delay = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(header); err == nil && at.After(now) {
		delay = at.Sub(now)
	}
	return delay
}
