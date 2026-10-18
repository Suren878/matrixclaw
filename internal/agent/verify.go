package agent

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
	"mvdan.cc/sh/v3/syntax"
)

// The built-in tools the verification check watches: bash runs checks, and the
// edit tools change files.
const bashTool = "bash"

var editTools = map[string]bool{"write": true, "edit": true, "multiedit": true, "apply_patch": true}

// docExtensions name files whose changes need no build or test run.
var docExtensions = map[string]bool{".md": true, ".markdown": true, ".mdx": true, ".txt": true, ".rst": true, ".adoc": true}

// observe records a finished call for the loop guard and the verification check.
func (r *run) observe(req callRequest, result tools.Result) {
	r.state.observeCall(req.name, req.args, result)
	r.state.observeChange(req.name, req.args, result)
}

// observeChange marks the run unverified when a call changed files other than
// docs (a successful edit, or a bash command that may write files), and
// verified when a successful foreground bash command ran a build, test, lint or
// type check.
func (s *RunState) observeChange(name string, args []byte, result tools.Result) {
	succeeded := ToolResultStatus(result) == tools.ResultStatusSuccess
	if editTools[name] {
		var params struct {
			FilePath string `json:"file_path"`
		}
		if succeeded && (json.Unmarshal(args, &params) != nil || !docExtensions[strings.ToLower(filepath.Ext(params.FilePath))]) {
			s.Unverified = true
		}
		return
	}
	var params tools.BashParams
	if name != bashTool || json.Unmarshal(args, &params) != nil {
		return
	}
	checks, writes := shellEffect(params.Command)
	meta, _ := result.Metadata.(tools.BashResponseMetadata)
	switch {
	case succeeded && checks && !params.RunInBackground && !meta.Background:
		s.Unverified = false
	case writes:
		s.Unverified = true
	}
}

// shellEffect reads a bash command line: whether one of its commands is a build,
// test, lint or type check, and whether one may write files.
func shellEffect(command string) (checks, writes bool) {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		words := strings.Fields(command)
		return checkCommand(words), writeCommand(words)
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			words := make([]string, 0, len(node.Args))
			for _, arg := range node.Args {
				words = append(words, wordText(arg))
			}
			checks = checks || checkCommand(words)
			writes = writes || writeCommand(words)
		case *syntax.Redirect:
			writes = writes || redirectWrites(node)
		}
		return true
	})
	return checks, writes
}

// wordText is a word's plain and quoted text; expansions read as nothing.
func wordText(word *syntax.Word) string {
	var text strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			text.WriteString(part.Value)
		case *syntax.SglQuoted:
			text.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					text.WriteString(lit.Value)
				}
			}
		}
	}
	return text.String()
}

// redirectWrites reports whether a redirection writes a file other than a
// device, a temporary file or a doc.
func redirectWrites(redirect *syntax.Redirect) bool {
	switch redirect.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll:
	default:
		return false
	}
	target := wordText(redirect.Word)
	return !strings.HasPrefix(target, "/dev/") && !strings.HasPrefix(target, "/tmp/") && !docExtensions[strings.ToLower(filepath.Ext(target))]
}

// commandWords drops what runs a command without being it: wrappers such as
// time or timeout and their options, env assignments and runners such as npx.
func commandWords(words []string) []string {
	for len(words) > 0 {
		switch filepath.Base(words[0]) {
		case "time", "nice", "nohup", "sudo", "command", "exec", "xvfb-run", "npx", "pnpx", "bunx":
			words = skipFlags(words[1:])
		case "timeout":
			words = skipFlags(words[1:])
			if len(words) > 0 {
				words = words[1:]
			}
		case "env":
			words = skipFlags(words[1:])
			for len(words) > 0 && strings.Contains(words[0], "=") {
				words = words[1:]
			}
		case "uv", "poetry", "pipenv", "pdm", "hatch", "bundle", "pnpm", "npm", "yarn", "bun":
			if len(words) > 1 && (words[1] == "run" && !isPackageManager(words[0]) || words[1] == "exec" || words[1] == "dlx") {
				words = skipFlags(words[2:])
				continue
			}
			return words
		default:
			return words
		}
	}
	return words
}

func isPackageManager(word string) bool {
	switch filepath.Base(word) {
	case "npm", "pnpm", "yarn", "bun":
		return true
	}
	return false
}

func skipFlags(words []string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") {
		words = words[1:]
	}
	return words
}

// subcommand is the first argument that is not a flag.
func subcommand(args []string) string {
	if args = skipFlags(args); len(args) > 0 {
		return args[0]
	}
	return ""
}

var checkTools = map[string]bool{
	"tsc": true, "vue-tsc": true, "vitest": true, "jest": true, "mocha": true, "eslint": true, "biome": true,
	"pytest": true, "py.test": true, "mypy": true, "pyright": true, "flake8": true, "pylint": true, "tox": true, "nox": true,
	"golangci-lint": true, "staticcheck": true, "rspec": true, "phpunit": true, "ctest": true, "ninja": true,
	"xcodebuild": true, "shellcheck": true,
}

// checkCommand reports whether a simple command is a build, test, lint or type check.
func checkCommand(words []string) bool {
	words = commandWords(words)
	if len(words) == 0 {
		return false
	}
	name, args := filepath.Base(words[0]), words[1:]
	sub := subcommand(args)
	switch name {
	case "go":
		return sub == "test" || sub == "build" || sub == "vet"
	case "cargo":
		return sub == "test" || sub == "build" || sub == "check" || sub == "clippy" || sub == "nextest"
	case "npm", "pnpm", "yarn", "bun":
		if sub == "run" || sub == "run-script" {
			sub = subcommand(args[slices.Index(args, sub)+1:])
		}
		return checkScript(sub)
	case "ruff":
		return sub == "check"
	case "python", "python3":
		module := ""
		if i := slices.Index(args, "-m"); i >= 0 && i+1 < len(args) {
			module = args[i+1]
		}
		return module == "pytest" || module == "unittest" || module == "mypy" || module == "compileall"
	case "make", "gmake", "just", "task":
		return sub == "" || checkScript(sub) || sub == "all" || sub == "ci" || sub == "verify"
	case "mvn", "mvnw":
		return slices.ContainsFunc(args, func(arg string) bool { return arg == "test" || arg == "verify" || arg == "package" || arg == "compile" })
	case "gradle", "gradlew":
		return slices.ContainsFunc(args, func(arg string) bool { return arg == "test" || arg == "build" || arg == "check" })
	case "dotnet", "swift", "bazel", "bazelisk", "zig":
		return sub == "test" || sub == "build"
	case "deno":
		return sub == "test" || sub == "check" || sub == "lint"
	case "mix":
		return sub == "test" || sub == "compile"
	case "cmake":
		return slices.Contains(args, "--build")
	}
	return checkTools[name] || strings.Contains(words[0], "/") && strings.Contains(name, "test")
}

// checkScript reports whether a package script or make target name is a check.
func checkScript(name string) bool {
	for _, word := range []string{"test", "build", "lint", "check", "typecheck", "tsc", "vet"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return name == "t"
}

// writeCommand reports whether a simple command may change files in place.
func writeCommand(words []string) bool {
	words = commandWords(words)
	if len(words) == 0 {
		return false
	}
	name, args := filepath.Base(words[0]), words[1:]
	switch name {
	case "sed", "perl":
		return slices.ContainsFunc(args, func(arg string) bool {
			return strings.HasPrefix(arg, "--in-place") || strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "i")
		})
	case "tee":
		return slices.ContainsFunc(args, func(arg string) bool { return !strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "/dev/") })
	case "patch", "mv", "cp", "rm", "ln", "touch", "truncate":
		return true
	case "git":
		switch subcommand(args) {
		case "apply", "am", "checkout", "switch", "restore", "merge", "rebase", "cherry-pick", "revert", "pull", "reset", "stash", "mv", "rm":
			return true
		}
	}
	return false
}
