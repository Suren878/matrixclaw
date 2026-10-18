package hooks

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/shelltask"
)

// outputLimit bounds what is kept of each output of a hook.
const outputLimit = 1 << 20

// Permission decisions of PreToolUse hooks.
const (
	Allow = "allow"
	Deny  = "deny"
	Ask   = "ask"
)

var (
	ErrNoProjectHooks = errors.New("this session's project has no " + ProjectFile)
	ErrProjectChanged = errors.New("the project hooks file changed since it was listed; list it again")
)

// Config is what the runner follows, read for every hook: setup.json's hooks
// and diagnostics.
type Config struct {
	Hooks       []Hook
	Diagnostics Diagnostics
}

// Runner runs setup.json's hooks, the hooks of trusted project files and the
// checks after edits as bash commands with the environment of shell commands.
type Runner struct {
	env    func() []string
	config func() Config
	trust  *trustStore
	logf   func(format string, args ...any)
	// reported holds the project files already logged as not run.
	reported sync.Map
}

// NewRunner returns a runner; env (nil for the daemon's) is the commands'
// environment, trustPath the file keeping trusted project hooks files.
func NewRunner(env func() []string, config func() Config, trustPath string) *Runner {
	return &Runner{env: env, config: config, trust: &trustStore{path: trustPath}, logf: log.Printf}
}

// Scope is the session a hook runs for.
type Scope struct {
	SessionID      string
	RunID          string
	WorkingDir     string
	PermissionMode string
}

// ToolCall is the call a tool hook runs for; Files are the resolved paths a
// file tool targets (several for apply_patch).
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
	Files []string
}

// ToolDecision is what the PreToolUse hooks of a call decided: Permission is
// Allow, Deny, Ask or "" for none, Input the arguments they rewrote the call
// to (nil keeps them), Context text for the model.
type ToolDecision struct {
	Permission string
	Reason     string
	Input      json.RawMessage
	Context    string
}

// PreToolUse runs the call's PreToolUse hooks in order, each seeing the input
// the ones before rewrote. Exit 2 or a deny ends the call at once; otherwise
// ask wins over allow.
func (r *Runner) PreToolUse(ctx context.Context, scope Scope, call ToolCall) ToolDecision {
	var decision ToolDecision
	var contexts []string
	for _, hook := range r.matching(PreToolUse, scope.WorkingDir, call.Name) {
		ran := r.run(ctx, scope, hook, input{ToolName: call.Name, ToolUseID: call.ID, ToolInput: call.Input}, call)
		if ran.exit == 2 {
			return ToolDecision{Permission: Deny, Reason: cmp.Or(clip(ran.stderr), "a PreToolUse hook blocked the call")}
		}
		out, ok := ran.output()
		if !ok {
			continue
		}
		specific := out.HookSpecificOutput
		permission, reason := specific.PermissionDecision, cmp.Or(specific.PermissionDecisionReason, out.Reason)
		switch {
		case out.stopped():
			permission, reason = Deny, cmp.Or(out.StopReason, reason)
		case out.Decision == "block":
			permission = Deny
		case out.Decision == "approve" && permission == "":
			permission = Allow
		}
		switch permission {
		case Deny:
			return ToolDecision{Permission: Deny, Reason: cmp.Or(clip(reason), "a PreToolUse hook denied the call")}
		case Ask:
			if decision.Permission != Ask {
				decision.Permission, decision.Reason = Ask, reason
			}
		case Allow:
			if decision.Permission == "" {
				decision.Permission, decision.Reason = Allow, reason
			}
		}
		if updated := specific.UpdatedInput; len(updated) > 0 && string(updated) != "null" {
			var object map[string]json.RawMessage
			if json.Unmarshal(updated, &object) != nil {
				r.logf("hook %s %q: updatedInput is not a JSON object; ignored", PreToolUse, hook.Command)
			} else {
				decision.Input, call.Input = updated, updated
			}
		}
		contexts = appendText(contexts, specific.AdditionalContext)
	}
	decision.Context = strings.Join(contexts, "\n\n")
	return decision
}

// PostToolUse runs the PostToolUse hooks of a finished call, then the check
// after an edit, and returns what they tell the model ("" for nothing).
func (r *Runner) PostToolUse(ctx context.Context, scope Scope, call ToolCall, response ToolResponse) string {
	var told []string
	for _, hook := range r.matching(PostToolUse, scope.WorkingDir, call.Name) {
		ran := r.run(ctx, scope, hook, input{ToolName: call.Name, ToolUseID: call.ID, ToolInput: call.Input, ToolResponse: &response}, call)
		if ran.exit == 2 {
			told = appendText(told, ran.stderr)
			continue
		}
		out, ok := ran.output()
		switch {
		case ran.exit != 0:
		case !ok:
			told = appendText(told, ran.stdout)
		default:
			if out.Decision == "block" {
				told = appendText(told, out.Reason)
			}
			if out.stopped() {
				told = appendText(told, out.StopReason)
			}
			told = appendText(told, out.HookSpecificOutput.AdditionalContext)
		}
	}
	var text strings.Builder
	for _, part := range told {
		fmt.Fprintf(&text, "\n\n<hook event=\"%s\">\n%s\n</hook>", PostToolUse, part)
	}
	if response.Status == "success" {
		if report := r.diagnose(ctx, scope, call); report != "" {
			text.WriteString("\n\n" + report)
		}
	}
	return strings.TrimPrefix(text.String(), "\n\n")
}

// Stop runs the Stop (or SubagentStop) hooks of a run that would end and
// returns why it must go on, "" to let it end; active says a Stop hook already
// kept it going.
func (r *Runner) Stop(ctx context.Context, scope Scope, event Event, active bool, reply string) string {
	var reasons []string
	for _, hook := range r.matching(event, scope.WorkingDir, "") {
		ran := r.run(ctx, scope, hook, input{StopHookActive: &active, LastAssistantMessage: reply}, ToolCall{})
		if ran.exit == 2 {
			reasons = append(reasons, cmp.Or(clip(ran.stderr), "a "+string(event)+" hook asks you to continue"))
			continue
		}
		out, ok := ran.output()
		switch {
		case !ok:
		case out.stopped():
			return ""
		case out.Decision == "block":
			reasons = append(reasons, cmp.Or(clip(out.Reason), "a "+string(event)+" hook asks you to continue"))
		}
	}
	return strings.Join(reasons, "\n\n")
}

// SessionStart runs the SessionStart hooks of source ("startup" for a session's
// first run, "resume" for a later one) and returns the context they print.
func (r *Runner) SessionStart(ctx context.Context, scope Scope, source string) string {
	var contexts []string
	for _, hook := range r.matching(SessionStart, scope.WorkingDir, source) {
		ran := r.run(ctx, scope, hook, input{Source: source}, ToolCall{})
		if ran.exit != 0 {
			continue
		}
		if out, ok := ran.output(); ok {
			contexts = appendText(contexts, out.HookSpecificOutput.AdditionalContext)
		} else {
			contexts = appendText(contexts, ran.stdout)
		}
	}
	return strings.Join(contexts, "\n\n")
}

// PreCompact runs the PreCompact hooks of trigger ("auto" or "manual") before a
// summary; they only observe.
func (r *Runner) PreCompact(ctx context.Context, scope Scope, trigger string) {
	for _, hook := range r.matching(PreCompact, scope.WorkingDir, trigger) {
		r.run(ctx, scope, hook, input{Trigger: trigger}, ToolCall{})
	}
}

// Notify runs the Notification hooks of kind ("permission_prompt") with message.
func (r *Runner) Notify(ctx context.Context, scope Scope, kind, message string) {
	for _, hook := range r.matching(Notification, scope.WorkingDir, kind) {
		r.run(ctx, scope, hook, input{Message: message, NotificationType: kind}, ToolCall{})
	}
}

// List is every hook a session in workingDir meets.
func (r *Runner) List(workingDir string) Listing {
	return Listing{Hooks: slices.Clone(r.config().Hooks), Project: readProject(workingDir, r.trust)}
}

// Trust trusts the project hooks file of workingDir while it has the SHA256
// sum the owner was shown.
func (r *Runner) Trust(workingDir, sum string) (Listing, error) {
	project := readProject(workingDir, r.trust)
	switch {
	case project == nil:
		return Listing{}, ErrNoProjectHooks
	case project.SHA256 != sum:
		return Listing{}, ErrProjectChanged
	case project.Error != "":
		return Listing{}, fmt.Errorf("%s: %s", project.Path, project.Error)
	}
	if err := r.trust.trust(project.Path, project.SHA256); err != nil {
		return Listing{}, err
	}
	return r.List(workingDir), nil
}

// boundHook is a hook that runs with dir as its project directory.
type boundHook struct {
	Hook
	dir string
}

// matching lists the hooks of event whose matcher takes subject: setup.json's,
// then those of the project file when its owner trusted it.
func (r *Runner) matching(event Event, workingDir, subject string) []boundHook {
	var out []boundHook
	add := func(hooks []Hook, dir string) {
		for _, hook := range hooks {
			if hook.Event != event {
				continue
			}
			if match, err := compileMatcher(hook.Matcher); err == nil && match(subject) {
				out = append(out, boundHook{Hook: hook, dir: dir})
			}
		}
	}
	add(r.config().Hooks, workingDir)
	project := readProject(workingDir, r.trust)
	switch {
	case project == nil:
	case project.Error != "":
		r.reportOnce(project, "is invalid, so none of its hooks run: "+project.Error)
	case !project.Trusted:
		r.reportOnce(project, "is not trusted, so its hooks do not run; the owner can trust it with /hooks")
	default:
		add(project.Hooks, project.Dir)
	}
	return out
}

func (r *Runner) reportOnce(project *ProjectHooks, text string) {
	if _, seen := r.reported.LoadOrStore(project.Path+"\x00"+project.SHA256, true); !seen {
		r.logf("hooks: %s %s", project.Path, text)
	}
}

// finished is how one hook ended; exit is -1 when it did not exit by itself.
type finished struct {
	exit   int
	stdout string
	stderr string
}

// output is the JSON stdout of a hook that exited 0.
func (r finished) output() (output, bool) {
	if r.exit != 0 {
		return output{}, false
	}
	return parseOutput([]byte(r.stdout))
}

// run runs one hook with in on stdin. Exits other than 0 and 2, timeouts and
// system messages are logged.
func (r *Runner) run(ctx context.Context, scope Scope, hook boundHook, in input, call ToolCall) finished {
	in.SessionID, in.RunID, in.Cwd, in.PermissionMode, in.HookEventName = scope.SessionID, scope.RunID, scope.WorkingDir, scope.PermissionMode, hook.Event
	stdin, err := json.Marshal(in)
	if err != nil {
		r.logf("hook %s %q: %v", hook.Event, hook.Command, err)
		return finished{exit: -1}
	}
	ctx, cancel := context.WithTimeout(ctx, hook.timeout())
	defer cancel()
	result, err := shelltask.Run(ctx, hook.Command, scope.WorkingDir, r.environ(scope, hook.dir, call), stdin, outputLimit)
	done := finished{exit: result.ExitCode, stdout: string(result.Stdout), stderr: string(result.Stderr)}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		r.logf("hook %s %q: timed out after %s", hook.Event, hook.Command, hook.timeout())
		done.exit = -1
	case err != nil:
		r.logf("hook %s %q: %v", hook.Event, hook.Command, err)
		done.exit = -1
	case done.exit != 0 && done.exit != 2:
		r.logf("hook %s %q: exit %d: %s", hook.Event, hook.Command, done.exit, strings.TrimSpace(done.stderr))
	}
	if out, ok := done.output(); ok && out.SystemMessage != "" {
		r.logf("hook %s %q: %s", hook.Event, hook.Command, out.SystemMessage)
	}
	return done
}

// environ is the environment of a hook: the commands' environment and the
// variables naming its session, project, tool and file.
func (r *Runner) environ(scope Scope, projectDir string, call ToolCall) []string {
	var env []string
	if r.env != nil {
		env = slices.Clone(r.env())
	} else {
		env = os.Environ()
	}
	env = append(env, "MATRIXCLAW_SESSION_ID="+scope.SessionID, "CLAUDE_PROJECT_DIR="+cmp.Or(projectDir, scope.WorkingDir))
	if call.Name != "" {
		env = append(env, "MATRIXCLAW_TOOL="+call.Name)
	}
	if len(call.Files) > 0 {
		env = append(env, "MATRIXCLAW_FILE="+call.Files[0])
	}
	return env
}

func appendText(texts []string, text string) []string {
	if text = clip(text); text != "" {
		return append(texts, text)
	}
	return texts
}
