package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/hooks"
)

const hooksUsage = "Usage: /hooks, /hooks trust"

func (d *Dispatcher) handleHooks(ctx context.Context, args string) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return textResult("Select or create a session first."), nil
	}
	if !core.CapabilitiesForSession(*session).NativeTools {
		return textResult("Hooks run in Matrixclaw sessions only."), nil
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		listing, err := d.daemon.SessionHooks(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Screen: InfoData{Title: "Hooks", Text: hooksText(listing)}}, nil
	}
	if strings.ToLower(fields[0]) != "trust" || len(fields) > 2 {
		return textResult(hooksUsage), nil
	}
	if !d.owner() {
		return textResult("Only the owner can trust a project's hooks."), nil
	}
	if len(fields) == 2 {
		listing, err := d.daemon.TrustSessionHooks(ctx, session.ID, fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Screen: InfoData{Title: "Hooks", Text: "Trusted. Its hooks run until the file changes.\n\n" + hooksText(listing)}}, nil
	}
	listing, err := d.daemon.SessionHooks(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	project := listing.Project
	switch {
	case project == nil:
		return textResult("This session's project has no " + hooks.ProjectFile + "."), nil
	case project.Error != "":
		return textResult(project.Path + " is invalid: " + project.Error), nil
	case project.Trusted:
		return textResult(project.Path + " is already trusted."), nil
	}
	return Result{Screen: ConfirmData{
		Title:          "Trust project hooks",
		Message:        "Run these commands of " + project.Path + " in this project's sessions?\n\n" + hookLines(project.Hooks),
		ConfirmLabel:   "Trust",
		CancelLabel:    "Close",
		ConfirmCommand: "/hooks trust " + project.SHA256,
		CancelCommand:  "/hooks",
		ConfirmDanger:  true,
	}}, nil
}

func hooksText(listing hooks.Listing) string {
	var text strings.Builder
	text.WriteString("setup.json:\n")
	if len(listing.Hooks) == 0 {
		text.WriteString("no hooks\n")
	}
	text.WriteString(hookLines(listing.Hooks))
	if project := listing.Project; project != nil {
		state := "trusted"
		switch {
		case project.Error != "":
			state = "invalid, none run: " + project.Error
		case !project.Trusted:
			state = "not trusted, none run; /hooks trust to trust it"
		}
		fmt.Fprintf(&text, "\n%s (%s):\n%s", project.Path, state, hookLines(project.Hooks))
	}
	return strings.TrimRight(text.String(), "\n") + "\n\n" + hooksUsage
}

func hookLines(list []hooks.Hook) string {
	var text strings.Builder
	for _, hook := range list {
		event := string(hook.Event)
		if hook.Matcher != "" {
			event += " " + hook.Matcher
		}
		fmt.Fprintf(&text, "• %s: %s\n", event, hook.Command)
	}
	return text.String()
}
