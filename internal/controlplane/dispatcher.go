package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

// Result is what a command shows, one screen or none, and whether the client
// reloads its snapshot because the command changed session state.
type Result struct {
	Screen         Screen
	ReloadSnapshot bool
}

func textResult(text string) Result {
	return Result{Screen: TextData{Text: text}}
}

// Text is the text of a TextData screen, "" for any other.
func (r Result) Text() string {
	text, _ := r.Screen.(TextData)
	return text.Text
}

// withPickerMeta sets the meta line of a picker screen.
func (r Result) withPickerMeta(meta string) Result {
	if picker, ok := r.Screen.(PickerData); ok {
		picker.Meta = meta
		r.Screen = picker
	}
	return r
}

// Dispatcher runs slash commands against the daemon for one client: its
// name, external key and role come with daemon. It keeps the client's open
// forms, so a client keeps one dispatcher while it shows its screens.
type Dispatcher struct {
	daemon     *daemonclient.Client
	workingDir string
	now        func() time.Time
	forms      *formStore
}

func New(daemon *daemonclient.Client, workingDir string) *Dispatcher {
	return &Dispatcher{daemon: daemon, workingDir: strings.TrimSpace(workingDir), now: time.Now, forms: newFormStore(time.Now)}
}

// Handle runs a typed or picked command line.
func (d *Dispatcher) Handle(ctx context.Context, line string) (Result, error) {
	route, ok := DecodeRoute(line)
	if !ok {
		return textResult("Unknown command.\n\n" + HelpText()), nil
	}
	return d.run(ctx, route)
}

// Answer runs the command a prompt submits with answer as its value; the
// answer never becomes part of a command line.
func (d *Dispatcher) Answer(ctx context.Context, submit string, answer string) (Result, error) {
	route, ok := DecodeRoute(submit)
	if !ok || !route.Verb.value || route.Value != "" {
		return textResult("This prompt can no longer be answered."), nil
	}
	route.Value = strings.TrimSpace(answer)
	return d.run(ctx, route)
}

func (d *Dispatcher) run(ctx context.Context, r Route) (Result, error) {
	switch r.Verb.cmd {
	case CommandHelp:
		return Result{Screen: commandMenuPicker()}, nil
	case CommandNewSession:
		return d.handleNewSession(ctx, r)
	case CommandSessions:
		return d.handleSessions(ctx)
	case CommandSession:
		return d.handleSession(ctx, r)
	case CommandProvider:
		return d.handleProvider(ctx, r)
	case CommandPermissions:
		return d.handlePermissions(ctx, r)
	case CommandPlan:
		return d.handlePlan(ctx, r)
	case CommandApproval:
		return d.handleApproval(ctx, r)
	case CommandContext:
		return d.handleContext(ctx, r)
	case CommandUsage:
		return d.handleUsage(ctx)
	case CommandContinue:
		return d.handleContinue(ctx)
	case CommandBudget:
		return d.handleBudget(ctx, r)
	case CommandGoal:
		return d.handleGoal(ctx, r)
	case CommandUndo:
		return d.handleUndo(ctx, r)
	case CommandRewind:
		return d.handleRewind(ctx, r)
	case CommandTodo:
		return d.handleTodo(ctx, r)
	case CommandHooks:
		return d.handleHooks(ctx, r)
	case CommandMemory:
		return d.handleMemory(ctx, r)
	case CommandSearch:
		return d.handleSearch(ctx, r)
	case CommandSkills:
		return d.handleSessionSkills(ctx, r)
	case CommandModules:
		return d.handleModules(ctx, r)
	case CommandRemind:
		return d.handleRemind(ctx, r)
	case CommandTasks:
		return d.handleTasks(ctx, r)
	case CommandServer:
		return d.handleServer(), nil
	case CommandStatus:
		return d.handleStatus(ctx)
	case CommandRestart:
		return d.handleRestart(), nil
	case CommandStop:
		return d.handleStop(ctx, r)
	default:
		return textResult("Unknown command.\n\n" + HelpText()), nil
	}
}
