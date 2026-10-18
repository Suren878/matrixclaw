package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

const (
	// diagnosticsTimeout bounds one check after an edit.
	diagnosticsTimeout = 20 * time.Second
	maxDiagnosedFiles  = 5
	maxDiagnosticLines = 30
	maxDiagnosticWidth = 500
)

// editTools are the tools after whose successful calls the edited files are checked.
var editTools = map[string]bool{"write": true, "edit": true, "multiedit": true, "apply_patch": true}

// check is a built-in check: command runs next to marker, the nearest such file
// from the edited file's directory up (skipped when there is none), or in the
// file's directory when marker is empty. A check skips itself when its tool is
// not installed.
type check struct {
	marker  string
	command string
}

var builtinChecks = map[string]check{
	".go": {command: `command -v go >/dev/null && command -v gofmt >/dev/null || exit 0
if ! listed=$(gofmt -l -e "$MATRIXCLAW_FILE" 2>&1); then printf '%s\n' "$listed"; exit 1; fi
[ -z "$listed" ] || printf '%s:1:1: the file is not gofmt-formatted\n' "$MATRIXCLAW_FILE"
go vet . 2>&1`},
	".ts":  {marker: "tsconfig.json", command: tscCheck},
	".tsx": {marker: "tsconfig.json", command: tscCheck},
	".py": {command: `py=$(command -v python3 || command -v python) || exit 0
"$py" - "$MATRIXCLAW_FILE" <<'EOF'
import sys
path = sys.argv[1]
try:
    compile(open(path, "rb").read(), path, "exec")
except (SyntaxError, ValueError) as e:
    print(f"{path}:{getattr(e, 'lineno', None) or 1}:{getattr(e, 'offset', None) or 1}: {type(e).__name__}: {getattr(e, 'msg', e)}")
EOF`},
	".rs": {marker: "Cargo.toml", command: `command -v cargo >/dev/null || exit 0
cargo check --quiet --message-format short 2>&1 | grep -v ': warning'`},
}

const tscCheck = `if [ -x node_modules/.bin/tsc ]; then tsc=node_modules/.bin/tsc; elif command -v tsc >/dev/null; then tsc=tsc; else exit 0; fi
"$tsc" --noEmit --pretty false -p . 2>&1`

// diagnose checks the files a successful edit changed, the first
// maxDiagnosedFiles of them within one diagnosticsTimeout, and reports their
// errors, "" when there are none or no check applies. A file goes to its
// language server unless its extension has a command of its own; the files no
// server checked get the command check. It never fails.
func (r *Runner) diagnose(ctx context.Context, scope Scope, call ToolCall) string {
	cfg := r.config().Diagnostics
	if cfg.Disabled || !editTools[call.Name] || len(call.Files) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, diagnosticsTimeout)
	defer cancel()
	files := call.Files[:min(len(call.Files), maxDiagnosedFiles)]
	var served []string
	for _, file := range files {
		if _, custom := cfg.Commands[strings.ToLower(filepath.Ext(file))]; !custom {
			served = append(served, file)
		}
	}
	servers := r.servers.Diagnose(ctx, served)
	var lines []string
	for _, file := range files {
		if !slices.Contains(servers.Checked, file) {
			lines = append(lines, r.diagnoseFile(ctx, scope, call, file)...)
		}
	}
	return report(servers.Lines(), lines, servers.OtherLines())
}

// diagnoseFile runs the check for file's language and keeps the lines naming it.
func (r *Runner) diagnoseFile(ctx context.Context, scope Scope, call ToolCall, file string) []string {
	cfg := r.config().Diagnostics
	ext := strings.ToLower(filepath.Ext(file))
	command, custom := cfg.Commands[ext]
	dir := scope.WorkingDir
	switch {
	case custom:
	case ext == ".json":
		return jsonErrors(file)
	default:
		builtin, ok := builtinChecks[ext]
		if !ok {
			return nil
		}
		if dir = checkDir(file, builtin.marker); dir == "" {
			return nil
		}
		command = builtin.command
	}
	if strings.TrimSpace(command) == "" {
		return nil
	}
	call.Files = []string{file}
	ran, err := shelltask.Run(ctx, command, dir, r.environ(scope, scope.WorkingDir, call), nil, outputLimit)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			r.logf("diagnostics %s: timed out after %s", file, diagnosticsTimeout)
		}
		return nil
	}
	return mentioning(string(ran.Stdout)+"\n"+string(ran.Stderr), file)
}

// checkDir is where a check with marker runs for file; "" when marker is not found.
func checkDir(file, marker string) string {
	dir := filepath.Dir(file)
	if marker == "" {
		return dir
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// mentioning keeps the lines of output that name file, by its absolute path or
// a path relative to a directory above it, with the indented lines after them.
func mentioning(output, file string) []string {
	var kept []string
	keeping := false
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case mentions(line, file):
			keeping = true
		case keeping && line != "" && (line[0] == ' ' || line[0] == '\t'):
		default:
			keeping = false
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// mentions reports whether line names file: its base name followed by ':', '('
// or the end, after nothing, "./" or a path its directory ends with.
func mentions(line, file string) bool {
	base := filepath.Base(file)
	dir := filepath.Dir(file) + string(filepath.Separator)
	for at := 0; ; {
		i := strings.Index(line[at:], base)
		if i < 0 {
			return false
		}
		i += at
		end := i + len(base)
		if end == len(line) || line[end] == ':' || line[end] == '(' {
			prefix := strings.TrimPrefix(line[strings.LastIndexAny(line[:i], " \t\"'")+1:i], "./")
			if prefix == "" || strings.HasSuffix(dir, prefix) && strings.HasSuffix(prefix, string(filepath.Separator)) {
				return true
			}
		}
		at = i + 1
	}
}

// report renders the diagnostics for the tool result: the language servers'
// errors in the edited files, at most maxDiagnosticLines of the commands'
// lines, then the servers' errors in other files.
func report(served, commands, others []string) string {
	if len(commands) > maxDiagnosticLines {
		commands = append(commands[:maxDiagnosticLines:maxDiagnosticLines], fmt.Sprintf("... and %d more lines", len(commands)-maxDiagnosticLines))
	}
	lines := slices.Concat(served, commands, others)
	if len(lines) == 0 {
		return ""
	}
	var text strings.Builder
	text.WriteString("Diagnostics after edit:")
	for _, line := range lines {
		text.WriteString("\n" + textutil.Clip(line, maxDiagnosticWidth, "..."))
	}
	return text.String()
}

// jsonErrors parses a JSON file; files that allow comments (tsconfig, VS Code
// settings) are left alone.
func jsonErrors(file string) []string {
	base := strings.ToLower(filepath.Base(file))
	if strings.HasPrefix(base, "tsconfig") || strings.HasPrefix(base, "jsconfig") || filepath.Base(filepath.Dir(file)) == ".vscode" {
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var value any
	err = json.Unmarshal(data, &value)
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		return nil
	}
	// Offset counts the byte the error is at.
	before := data[:min(max(int(syntax.Offset)-1, 0), len(data))]
	line := strings.Count(string(before), "\n") + 1
	column := len(before) - strings.LastIndexByte(string(before), '\n')
	return []string{fmt.Sprintf("%s:%d:%d: %s", file, line, column, syntax.Error())}
}
