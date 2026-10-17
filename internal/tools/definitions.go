package tools

// Definition is a core tool; one whose executor needs the daemon's services
// (the shell tools) has no NewExecutor and is registered on its own.
type Definition struct {
	Spec        Spec
	NewExecutor func() Executor
}

// CoreExecutors returns the core tools that need no daemon services.
func CoreExecutors() []Executor {
	executors := make([]Executor, 0, len(coreDefinitions))
	for _, definition := range coreDefinitions {
		if definition.NewExecutor != nil {
			executors = append(executors, definition.NewExecutor())
		}
	}
	return executors
}

func coreDefinitionSpec(id string) Spec {
	for _, definition := range coreDefinitions {
		if definition.Spec.ID == id {
			return cloneSpec(definition.Spec)
		}
	}
	return Spec{}
}

var coreDefinitions = []Definition{
	{
		Spec: Spec{
			ID:              readToolName,
			Description:     "Read a file with line numbers",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: readInputSchema,
		},
		NewExecutor: NewReadExecutor,
	},
	{
		Spec: Spec{
			ID:              globToolName,
			Description:     "Find files by path pattern",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: globInputSchema,
		},
		NewExecutor: NewGlobExecutor,
	},
	{
		Spec: Spec{
			ID:              grepToolName,
			Description:     "Search file contents by pattern",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: grepInputSchema,
		},
		NewExecutor: NewGrepExecutor,
	},
	{
		Spec: Spec{
			ID:              lsToolName,
			Description:     "List files in a tree",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: lsInputSchema,
		},
		NewExecutor: NewLSExecutor,
	},
	{
		Spec: Spec{
			ID:              writeToolName,
			Description:     "Create or replace a file",
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: writeInputSchema,
		},
		NewExecutor: NewWriteExecutor,
	},
	{
		Spec: Spec{
			ID:              editToolName,
			Description:     "Replace content inside an existing file",
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: editInputSchema,
		},
		NewExecutor: NewEditExecutor,
	},
	{
		Spec: Spec{
			ID:              multiEditToolName,
			Description:     "Apply several edits to one file",
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: multiEditInputSchema,
		},
		NewExecutor: NewMultiEditExecutor,
	},
	{
		Spec: Spec{
			ID:              bashToolName,
			Description:     bashDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: bashInputSchema,
		},
	},
	{
		Spec: Spec{
			ID:              taskOutputToolName,
			Description:     "Read a background task's output produced since your last read, with its status, and its exit code once it ended. Works for bash commands started with run_in_background or moved to the background, and for background agent children. Set wait_seconds to block until the task finishes (or the wait ends) instead of polling in a loop. Set filter to a regular expression to keep only matching lines, such as FAIL|panic|error. For a long finished output, read or grep its output file instead.",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: taskOutputInputSchema,
		},
	},
	{
		Spec: Spec{
			ID:              taskKillToolName,
			Description:     "Stop a running background task by id: a shell command with every process it started, or a background agent child. Stop the servers and watchers you started once you no longer need them.",
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: taskKillInputSchema,
		},
	},
}

const bashDescription = "Run a shell command with bash -c and return its combined stdout and stderr followed by its exit code. " +
	"Each call is a fresh non-interactive shell in the working directory (or working_dir): cd and exported variables do not carry over, so use working_dir or absolute paths. " +
	"There is no TTY and stdin is empty: use non-interactive flags (-y, --no-pager, git commit -m) and never open editors, pagers or interactive modes (git rebase -i, git add -p). " +
	"Output over 30 KB is cut to its head and tail, with the path of a file holding all of it. " +
	"A foreground command still running after auto_background_after seconds (default 120) moves to the background as a task; one running past timeout seconds (default 600, at most 3600) is killed, and so are processes a finished command left running. " +
	"Start servers, watchers, builds and full test suites with run_in_background: true, then read them with task_output (or wait with await) and stop them with task_kill. " +
	"Do not use bash to read, search or edit files: use read, grep, glob, ls, edit and write. Provider API keys are not in the environment."

const readOnlyBashDescription = "Run a read-only shell command with bash -c and return its output and exit code. You are read-only, so only these commands run, joined with pipes, && or ;: " +
	"git status, log, diff, show, blame, grep, ls-files, rev-parse; go list, vet, doc, env; ls, cat, head, tail, wc, rg, grep, find (without -exec or -delete), tree, jq, stat, diff, sort, cut, tr. " +
	"Paths must stay inside the working directory; redirections to files, variables, substitutions and unquoted globs are refused. " +
	"Each call is a fresh shell without a TTY. Prefer read, grep and glob for files; use this for git history and go tooling."
