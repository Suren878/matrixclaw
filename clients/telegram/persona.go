package telegram

import "strings"

var telegramPersonaReplacer = strings.NewReplacer(
	"Restart server daemon?", "Restart Architect?",
	"Server daemon restart requested.", "Architect restart requested.",
	"Daemon is restarting...", "Architect is restarting...",
	"Daemon restart failed", "Architect restart failed",
	"Daemon restarted.", "Architect restarted.",
	"Restart Daemon", "Restart Architect",
)

// telegramPersonaText is only for Telegram user-facing copy. Internal code,
// logs, API names, and daemon concepts keep their technical names.
func telegramPersonaText(text string) string {
	return telegramPersonaReplacer.Replace(text)
}
