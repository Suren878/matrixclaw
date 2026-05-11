package telegram

import "testing"

func TestTelegramPersonaTextKeepsTechnicalCopyScoped(t *testing.T) {
	tests := map[string]string{
		"Restart server daemon?":           "Restart Architect?",
		"Server daemon restart requested.": "Architect restart requested.",
		"Restart Daemon":                   "Restart Architect",
		"Daemon is restarting...":          "Architect is restarting...",
		"Daemon restart failed: boom":      "Architect restart failed: boom",
		"daemon API":                       "daemon API",
	}

	for input, want := range tests {
		if got := telegramPersonaText(input); got != want {
			t.Fatalf("telegramPersonaText(%q) = %q, want %q", input, got, want)
		}
	}
}
