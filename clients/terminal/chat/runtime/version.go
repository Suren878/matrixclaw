package runtime

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/version"
)

func runtimeVersion(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	return version.String()
}
