package webresearch

import "strings"

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusRunning:
		return StatusRunning
	case StatusCompleted:
		return StatusCompleted
	case StatusFailed:
		return StatusFailed
	default:
		return StatusPending
	}
}
