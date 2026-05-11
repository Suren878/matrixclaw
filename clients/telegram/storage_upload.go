package telegram

import (
	"strings"
)

const maxTelegramStorageUploadBytes = 25 * 1024 * 1024

func safeStorageFileName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Trim(name, "/")
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	parts := strings.Split(name, "/")
	name = strings.TrimSpace(parts[len(parts)-1])
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}
