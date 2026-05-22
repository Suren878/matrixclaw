package setup

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func cloneDraft(d setup.Draft) setup.Draft {
	out := d
	if d.Providers != nil {
		out.Providers = append([]setup.ProviderDraft(nil), d.Providers...)
	}
	return out
}
