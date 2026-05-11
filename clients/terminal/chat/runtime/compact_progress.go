package runtime

import "strings"

const compactProgressText = "Compacting context..."

func isContextCompactCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(command), "/context compact confirm")
}
