package permission

import (
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Line is a parsed bash command line: the words of every simple command (those in
// substitutions too; a word that is not plain text is ""), and Risky when no
// pattern rule may allow it: it writes files, assigns, substitutes, runs a wrapper
// or a flag that runs a program, or does not parse.
type Line struct {
	Commands [][]string
	Risky    bool
}

// wrappers run another command given as their arguments.
var wrappers = map[string]bool{
	".": true, "bash": true, "builtin": true, "busybox": true, "chroot": true, "command": true,
	"dash": true, "doas": true, "env": true, "eval": true, "exec": true, "fish": true,
	"ionice": true, "ksh": true, "ltrace": true, "nice": true, "nohup": true, "parallel": true,
	"script": true, "setsid": true, "sh": true, "source": true, "stdbuf": true, "strace": true,
	"su": true, "sudo": true, "time": true, "timeout": true, "watch": true, "xargs": true, "zsh": true,
}

// flagSet names the flags of one command that run another program or write a
// file they name: long names without dashes, and short letters that also count
// inside a cluster such as "-xIcmd".
type flagSet struct {
	names   []string
	letters string
}

// programFlags are keyed by command, "command subcommand", or "" for every command.
var programFlags = map[string]flagSet{
	"": {names: []string{
		"exec", "execdir", "ok", "okdir", "toolexec", "vettool", "eval", "pre", "pager", "extcmd",
		"to-command", "checkpoint-action", "info-script", "new-volume-script", "use-compress-program",
		"compress-program", "unzip-command", "diff-program", "rsh", "rsh-command", "rmt-command",
		"rsync-path", "upload-pack", "receive-pack", "open-files-in-pager", "config-env", "exec-path",
		"sendmail-cmd", "to-cmd", "cc-cmd", "output", "output-document", "log-file",
	}},
	"go":        {names: []string{"o"}},
	"find":      {names: []string{"delete", "fprint", "fprint0", "fprintf", "fls"}},
	"git":       {letters: "cxO"},
	"git clone": {letters: "u"},
	"tar":       {letters: "IF"},
	"rsync":     {letters: "e"},
	"ssh":       {letters: "oFS"},
	"scp":       {letters: "oFS"},
	"sftp":      {letters: "oFS"},
	"man":       {letters: "PH"},
	"less":      {letters: "oO"},
	"sort":      {letters: "o"},
	"zip":       {letters: "T"},
	"curl":      {names: []string{"config"}, letters: "oK"},
	"wget":      {letters: "O"},
	"make":      {letters: "E"},
}

// ParseLine parses a bash command line; one that does not parse keeps its
// whitespace-separated words as a single command, so deny rules still see them.
func ParseLine(text string) Line {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		return Line{Commands: [][]string{strings.Fields(text)}, Risky: true}
	}
	var line Line
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			line.addCall(node)
		case *syntax.Redirect:
			if writesFile(node) {
				line.Risky = true
			}
		case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.DeclClause, *syntax.FuncDecl, *syntax.CoprocClause:
			line.Risky = true
		}
		return true
	})
	return line
}

func (l *Line) addCall(call *syntax.CallExpr) {
	if len(call.Assigns) > 0 {
		l.Risky = true
	}
	if len(call.Args) == 0 {
		return
	}
	words := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		word, ok := literal(arg)
		if !ok {
			l.Risky = true
		}
		words = append(words, word)
	}
	if words[0] == "" || wrappers[filepath.Base(words[0])] || runsProgram(words) {
		l.Risky = true
	}
	l.Commands = append(l.Commands, words)
}

// runsProgram reports whether a command passes one of its programFlags, gives
// tar an old-style cluster with one, or overrides a make variable.
func runsProgram(words []string) bool {
	command := filepath.Base(words[0])
	sets := []flagSet{programFlags[""], programFlags[command]}
	for _, word := range words[1:] {
		if set, ok := programFlags[command+" "+word]; ok {
			sets = append(sets, set)
		}
	}
	for i, word := range words[1:] {
		switch {
		case strings.HasPrefix(word, "-"):
			name, _, _ := strings.Cut(strings.TrimLeft(word, "-"), "=")
			for _, set := range sets {
				if slices.Contains(set.names, name) || !strings.HasPrefix(word, "--") && strings.ContainsAny(word[1:], set.letters) {
					return true
				}
			}
		case command == "tar" && i == 0 && strings.ContainsAny(word, programFlags["tar"].letters):
			return true
		case command == "make" && strings.Contains(word, "="):
			return true
		}
	}
	return false
}

// writesFile reports whether a redirection writes a file; /dev/null and
// duplicated descriptors do not count.
func writesFile(redirect *syntax.Redirect) bool {
	switch redirect.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob, syntax.RdrInOut:
		target, ok := literal(redirect.Word)
		return !ok || target != "/dev/null"
	case syntax.DplOut:
		target, ok := literal(redirect.Word)
		return !ok || !descriptor(target)
	default:
		return false
	}
}

func descriptor(target string) bool {
	if target == "-" {
		return true
	}
	for _, r := range target {
		if r < '0' || r > '9' {
			return false
		}
	}
	return target != ""
}

// literal is the text of a word made only of plain, quoted and escaped text.
func literal(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var text strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			text.WriteString(unescape(part.Value, false))
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false
			}
			text.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}
				text.WriteString(unescape(lit.Value, true))
			}
		default:
			return "", false
		}
	}
	return text.String(), true
}

// unescape drops the backslashes bash removes: before any character outside
// quotes, and before $, `, ", \ and a newline inside double quotes.
func unescape(value string, quoted bool) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var out strings.Builder
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' || i+1 == len(runes) {
			out.WriteRune(runes[i])
			continue
		}
		next := runes[i+1]
		if quoted && !strings.ContainsRune("$`\"\\\n", next) {
			out.WriteRune(runes[i])
			continue
		}
		i++
		if next != '\n' {
			out.WriteRune(next)
		}
	}
	return out.String()
}
