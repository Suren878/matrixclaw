// Package textutil holds small string helpers shared across packages.
package textutil

import "strings"

// FirstNonEmpty returns the first value that is not blank, trimmed.
// Use cmp.Or instead when the values are already trimmed.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
