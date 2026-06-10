package controlplane

import "strings"

func HelpText() string {
	lines := []string{"Commands:"}
	for _, view := range CommandMenuView(SurfaceTelegramBotCommands, MenuState{}).Items {
		lines = append(lines, view.Command+" - "+view.Title)
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
	fullCommand := strings.ToLower(strings.TrimSpace(command))
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(text[len(parts[0]):])
		fullCommand = fullCommand + " " + strings.ToLower(strings.TrimSpace(parts[1]))
	}
	for _, spec := range Catalog() {
		for _, alias := range spec.Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), fullCommand) {
				return spec, args, true
			}
			if strings.EqualFold(strings.TrimSpace(alias), command) {
				return spec, args, true
			}
		}
	}
	for _, spec := range Catalog() {
		if strings.EqualFold(strings.TrimPrefix(spec.Command, "/"), command) {
			return spec, args, true
		}
	}
	return CommandSpec{}, "", false
}
