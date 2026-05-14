package commandcatalog

import "testing"

func TestCommandLineUsesCatalogCommands(t *testing.T) {
	if got := CommandLine(CommandSessions, ""); got != "/sessions" {
		t.Fatalf("CommandLine(CommandSessions) = %q, want /sessions", got)
	}
	if got := CommandLine(CommandContext, "compact confirm"); got != "/context compact confirm" {
		t.Fatalf("CommandLine(CommandContext, compact confirm) = %q", got)
	}
	if got := CommandLine(CommandID("missing"), "args"); got != "" {
		t.Fatalf("CommandLine(missing) = %q, want empty string", got)
	}
}
