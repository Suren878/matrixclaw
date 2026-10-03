package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

type Result struct {
	Handled        bool
	Text           string
	ReloadSnapshot bool
	Picker         *PickerData
	Form           *FormData
	Prompt         *PromptData
	TextEdit       *TextEditData
	Confirm        *ConfirmData
	Info           *InfoData
}

// Dispatcher runs slash commands against the daemon for one client: its
// name, external key and role come with daemon.
type Dispatcher struct {
	daemon     *daemonclient.Client
	workingDir string
	now        func() time.Time
}

func New(daemon *daemonclient.Client, workingDir string) *Dispatcher {
	return &Dispatcher{daemon: daemon, workingDir: strings.TrimSpace(workingDir), now: time.Now}
}

func (d *Dispatcher) Handle(ctx context.Context, text string) (Result, error) {
	spec, args, ok := Parse(text)
	if !ok {
		return Result{}, nil
	}

	switch spec.ID {
	case CommandHelp:
		return Result{Handled: true, Picker: commandMenuPicker()}, nil
	case CommandNewSession:
		return d.handleNewSession(ctx, args)
	case CommandSessions:
		return d.handleSessions(ctx)
	case CommandSession:
		return d.handleSession(ctx, args)
	case CommandProvider:
		return d.handleProvider(ctx, args)
	case CommandPermissions:
		return d.handlePermissions(ctx, args)
	case CommandApproval:
		return d.handleApproval(ctx, args)
	case CommandContext:
		return d.handleContext(ctx, args)
	case CommandUsage:
		return d.handleUsage(ctx)
	case CommandContinue:
		return d.handleContinue(ctx)
	case CommandBudget:
		return d.handleBudget(ctx, args)
	case CommandTodo:
		return d.handleTodo(ctx, args)
	case CommandMemory:
		return d.handleMemory(ctx, args)
	case CommandSearch:
		return d.handleSearch(ctx, args)
	case CommandSkills:
		return d.handleSessionSkills(ctx, args)
	case CommandModules:
		return d.handleModules(ctx, args)
	case CommandRemind:
		return d.handleRemind(ctx, args)
	case CommandTasks:
		return d.handleTasks(ctx, args)
	case CommandServer:
		return d.handleServer(), nil
	case CommandStatus:
		return d.handleStatus(ctx)
	case CommandRestart:
		return d.handleRestart(), nil
	case CommandStop:
		return d.handleStop(ctx, args)
	default:
		return Result{Handled: true, Text: "Unknown command.\n\n" + HelpText()}, nil
	}
}
