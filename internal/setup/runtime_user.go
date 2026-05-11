package setup

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func userLingerEnabled(ctx context.Context, username string) (bool, error) {
	if strings.TrimSpace(username) == "" {
		return false, errors.New("empty username")
	}
	if _, err := exec.LookPath("loginctl"); err != nil {
		return false, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "loginctl", "show-user", username, "-p", "Linger", "--value")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "yes", nil
}
