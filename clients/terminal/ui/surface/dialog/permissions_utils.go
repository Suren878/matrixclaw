package dialog

import (
	"os"
	"strings"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/stringext"
)

func prettyName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return stringext.Capitalize(name)
}

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
