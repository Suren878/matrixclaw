package dialog

import (
	"os"
	"strings"
)

func prettyPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~/" + strings.TrimPrefix(path, home+"/")
		}
	}
	return path
}
