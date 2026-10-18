package permission

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// readOnlyCommands only read files or print: each command runs with any
// arguments (ParseLine already refuses the flags that write or run programs).
var readOnlyCommands = map[string]bool{
	"basename": true, "cat": true, "cd": true, "cmp": true, "comm": true, "cut": true, "diff": true,
	"dirname": true, "du": true, "echo": true, "egrep": true, "false": true, "fgrep": true, "file": true,
	"find": true, "grep": true, "head": true, "jq": true, "ls": true, "nl": true, "printf": true,
	"pwd": true, "readlink": true, "realpath": true, "rg": true, "sort": true, "stat": true, "tail": true,
	"test": true, "tr": true, "tree": true, "true": true, "wc": true, "which": true, "[": true,
}

// readOnlySubcommands are the subcommands of git and go that only read, each
// with the arguments it may take.
var readOnlySubcommands = map[string]map[string]func([]string) bool{
	"git": {
		"blame": anyArgs, "cat-file": anyArgs, "describe": anyArgs, "diff": anyArgs, "grep": anyArgs,
		"log": anyArgs, "ls-files": anyArgs, "ls-tree": anyArgs, "merge-base": anyArgs, "rev-list": anyArgs,
		"rev-parse": anyArgs, "shortlog": anyArgs, "show": anyArgs, "status": anyArgs,
		"branch": onlyArgs("-a", "-r", "-v", "-vv", "--all", "--remotes", "--verbose", "--list", "--show-current"),
		"remote": onlyArgs("-v", "--verbose"),
		"stash":  func(args []string) bool { return len(args) > 0 && (args[0] == "list" || args[0] == "show") },
		"tag":    onlyArgs("-l", "--list"),
	},
	"go": {
		"doc": anyArgs, "list": anyArgs, "version": anyArgs, "vet": anyArgs,
		"env": func(args []string) bool {
			return !slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "-") && arg != "-json" })
		},
	},
}

func anyArgs([]string) bool { return true }

// onlyArgs allows the listed arguments and no others.
func onlyArgs(allowed ...string) func([]string) bool {
	return func(args []string) bool {
		for _, arg := range args {
			if !slices.Contains(allowed, arg) {
				return false
			}
		}
		return true
	}
}

// gitGlobalOptions take a value as the next word.
var gitGlobalOptions = map[string]bool{"-C": true, "--git-dir": true, "--work-tree": true}

// CheckReadOnly reports why a bash command line may change something, or nil
// when it only reads: plain syntax with no file redirections, and every simple
// command a read-only one.
func CheckReadOnly(text string) error {
	line := ParseLine(text)
	if line.Risky {
		return errors.New("the command line uses a construct that may write or run other programs (a redirection to a file, variable, substitution, unquoted glob, wrapper or exec flag); quote patterns and keep to plain read-only commands")
	}
	if len(line.Commands) == 0 {
		return errors.New("the command line runs no command")
	}
	for _, words := range line.Commands {
		if !readOnlyCommand(words) {
			return fmt.Errorf("%q is not a read-only command; allowed: git status/log/diff/show/blame/grep/ls-files/rev-parse, go list/vet/doc, ls, cat, head, tail, wc, rg, grep, find, tree, jq, stat, diff, sort", strings.Join(words, " "))
		}
	}
	return nil
}

func readOnlyCommand(words []string) bool {
	name := words[0]
	if readOnlyCommands[name] {
		return true
	}
	subcommands, ok := readOnlySubcommands[name]
	if !ok {
		return false
	}
	args := words[1:]
	if name == "git" {
		args = skipGitGlobalOptions(args)
	}
	if len(args) == 0 {
		return false
	}
	allowed, ok := subcommands[args[0]]
	return ok && allowed(args[1:])
}

// skipGitGlobalOptions drops the options before git's subcommand.
func skipGitGlobalOptions(args []string) []string {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if gitGlobalOptions[args[0]] && len(args) > 1 {
			args = args[1:]
		}
		args = args[1:]
	}
	return args
}
