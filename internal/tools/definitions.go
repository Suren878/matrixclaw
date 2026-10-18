package tools

// Definition is a core tool; one whose executor needs the daemon's services
// (the shell tools) has no constructor and is registered on its own. The file
// tools that read and change files share one fileLedger.
type Definition struct {
	Spec            Spec
	NewExecutor     func() Executor
	newFileExecutor func(files *fileLedger) Executor
}

// CoreExecutors returns the core tools that need no daemon services.
func CoreExecutors() []Executor {
	files := newFileLedger()
	executors := make([]Executor, 0, len(coreDefinitions))
	for _, definition := range coreDefinitions {
		switch {
		case definition.newFileExecutor != nil:
			executors = append(executors, definition.newFileExecutor(files))
		case definition.NewExecutor != nil:
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

const (
	globDescription = "Find files by path pattern; the fastest way to locate files by name. " +
		"`*` matches within one path element, `**` any number of directories including none, `?` one character, " +
		"`[abc]` one of a set and `{a,b}` alternatives. Examples: `**/*.go`, `src/**/*.{ts,tsx}`, `cmd/*/main.go`. " +
		"The pattern is matched against paths relative to `path` (default: the working directory), so `*.go` finds only files directly in it " +
		"and `**/*.go` finds them at any depth. Returns up to 100 absolute file paths, most recently modified first. " +
		"Leaves out hidden entries unless the pattern names them (`.github/**`), git-ignored files and, outside a git repository, " +
		"dependency and build directories such as node_modules; a pattern or path that starts inside one of them searches it. " +
		"To search file contents use grep; to see a directory tree use ls."
	grepDescription = "Search file contents with a regular expression in Go RE2 syntax (no lookaround or backreferences; " +
		"escape literal braces and parentheses, as in `interface\\{\\}`), or for exact text with `literal_text`. " +
		"Searches `path`, a directory or one file (default: the working directory). " +
		"`include` limits the files: a glob without `/` matches file names at any depth (`*.go`, `*.{ts,tsx}`), " +
		"one with `/` matches paths relative to `path` (`src/**/*.ts`). " +
		"`output_mode` is `content` (default: matching lines with line numbers, grouped by file), `files_with_matches` (paths only) " +
		"or `count` (matches per file). Files are ordered most recently modified first, then by path and line; " +
		"at most `limit` results (default 100) are shown and the result says how many there are, continue with `offset`. " +
		"Leaves out hidden entries unless `include` names them, git-ignored paths, binary files, files over 5 MB and credential files. " +
		"Use this tool instead of grep or rg in bash."
	lsDescription = "List a directory as a tree; directories end with `/`. Shows `depth` levels (default 3; 1 is only the directory's own entries) " +
		"and at most 1000 entries, shallower levels first. Leaves out hidden entries, git-ignored paths and, outside a git repository, " +
		"dependency and build directories such as node_modules. `ignore` globs leave out more: a glob without `/` matches names at any depth " +
		"(`*.log`, `testdata`), one with `/` matches paths relative to `path`. For a file it gives its size. " +
		"Use glob to find files by pattern and read to see a file."
	readDescription = `Read a text file. Each line comes back prefixed with its line number and a tab; the prefix is not part of the file, so never copy it into edit strings. ` +
		`One call returns at most 2000 lines and about 24 KB; when more follows, the result ends with the offset to pass next. Lines over 2000 characters are cut. ` +
		`For a long file, locate the part you need with grep first and read it with offset and limit. Binary files are refused. ` +
		`Read a file before you change it; read several files in one reply when you need them. For directories use ls.`
	writeDescription = `Create a file, or replace an existing one completely, with content. Missing parent directories are created. ` +
		`An existing file must have been read in this session and not changed since, or the call is refused. ` +
		`To change part of a file use your edit tool instead: it sends only the changed text and cannot drop the rest by accident. ` +
		`Do not create documentation or README files unless asked.`
	editDescription = `Replace a piece of text in an existing file. old_string should be copied from read output exactly (indentation, whitespace and line breaks, without the line-number prefix) ` +
		`and must occur exactly once unless replace_all is true; include enough surrounding lines to make it unique. new_string must differ from old_string. ` +
		`If old_string is not found byte for byte, a match ignoring indentation or surrounding whitespace is tried, and the result says so; a miss shows the closest lines. ` +
		`The file must have been read in this session and not changed since. The result shows the changed lines with their line numbers, so there is no need to read the file again to check. Use write to create files.`
	applyPatchDescription = "Edit files with a patch: add, update, move and delete any number of files in one atomic call. " +
		"`input` is the whole patch:\n" +
		"*** Begin Patch\n" +
		"*** Add File: path/new.py\n" +
		"+every line of the new file starts with +\n" +
		"*** Update File: path/old.py\n" +
		"*** Move to: path/renamed.py   (optional)\n" +
		"@@ def handler():   (a line just above the change, to find the place; @@ alone when not needed)\n" +
		" unchanged line (starts with a space)\n" +
		"-removed line\n" +
		"+added line\n" +
		"*** Delete File: path/obsolete.py\n" +
		"*** End Patch\n" +
		"Give about 3 unchanged lines before and after each change; add another @@ line when they are not unique. " +
		"Chunks of one file follow the file's order. Paths are relative to the working directory or absolute. " +
		"Lines are matched exactly, then ignoring surrounding whitespace and quote or dash style. " +
		"Nothing is written unless every section applies. A file you update, move, delete or replace must have been read in this session and not changed since. " +
		"The result shows the changed lines with their line numbers."
	multiEditDescription = `Apply several replacements to one file in one atomic call: edits apply in order, each to the result of the one before, and if any edit fails nothing is written. ` +
		`Each edit follows the rules of edit. Prefer this to several edit calls on the same file.`
)

var coreDefinitions = []Definition{
	{
		Spec: Spec{
			ID:              readToolName,
			Description:     readDescription,
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: readInputSchema,
		},
		newFileExecutor: newReadExecutor,
	},
	{
		Spec: Spec{
			ID:              globToolName,
			Description:     globDescription,
			Effect:          EffectReadOnly,
			CoveredBy:       []string{readToolName},
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: globInputSchema,
		},
		NewExecutor: NewGlobExecutor,
	},
	{
		Spec: Spec{
			ID:              grepToolName,
			Description:     grepDescription,
			Effect:          EffectReadOnly,
			CoveredBy:       []string{readToolName},
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: grepInputSchema,
		},
		NewExecutor: NewGrepExecutor,
	},
	{
		Spec: Spec{
			ID:              lsToolName,
			Description:     lsDescription,
			Effect:          EffectReadOnly,
			CoveredBy:       []string{readToolName},
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: lsInputSchema,
		},
		NewExecutor: NewLSExecutor,
	},
	{
		Spec: Spec{
			ID:              writeToolName,
			Description:     writeDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: writeInputSchema,
		},
		newFileExecutor: newWriteExecutor,
	},
	{
		Spec: Spec{
			ID:              editToolName,
			Description:     editDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: editInputSchema,
		},
		newFileExecutor: newEditExecutor,
	},
	{
		Spec: Spec{
			ID:              multiEditToolName,
			Description:     multiEditDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: multiEditInputSchema,
		},
		newFileExecutor: newMultiEditExecutor,
	},
	{
		Spec: Spec{
			ID:              applyPatchToolName,
			Description:     applyPatchDescription,
			Effect:          EffectMutation,
			Asks:            true,
			CoveredBy:       []string{editToolName},
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: applyPatchInputSchema,
		},
		newFileExecutor: newApplyPatchExecutor,
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
