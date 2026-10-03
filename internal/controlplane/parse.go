package controlplane

import (
	"slices"
	"strings"
)

// BotCommands are the commands a client lists by name: the menu ones and /help.
func BotCommands() []CommandSpec {
	var out []CommandSpec
	for _, spec := range Catalog() {
		if spec.Public && (spec.Menu || spec.ID == CommandHelp) {
			out = append(out, spec)
		}
	}
	return out
}

func HelpText() string {
	lines := []string{"Commands:"}
	for _, spec := range BotCommands() {
		lines = append(lines, spec.Command+" - "+spec.Title)
	}
	return strings.Join(lines, "\n")
}

func CommandName(command string) string {
	return strings.TrimPrefix(strings.TrimSpace(command), "/")
}

func Parse(text string) (CommandSpec, string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return CommandSpec{}, "", false
	}
	text = strings.TrimPrefix(text, "/")
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return CommandSpec{}, "", false
	}
	command := parts[0]
	if idx := strings.IndexByte(command, '@'); idx >= 0 {
		command = command[:idx]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(text[len(parts[0]):])
	}
	for _, spec := range Catalog() {
		if strings.EqualFold(strings.TrimPrefix(spec.Command, "/"), command) || slices.ContainsFunc(spec.Aliases, func(alias string) bool {
			return strings.EqualFold(alias, command)
		}) {
			return spec, args, true
		}
	}
	return CommandSpec{}, "", false
}
