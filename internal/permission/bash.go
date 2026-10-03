package permission

import (
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Line is a parsed bash command line: the words of every simple command (those in
// substitutions too; a word that is not plain text is ""), and Risky when no
// pattern rule may allow it: it writes files, assigns, substitutes, expands
// braces or globs, runs a wrapper or a flag that runs a program, or does not parse.
type Line struct {
	Commands [][]string
	Risky    bool
}

// wrappers run a command given as their arguments, or change which program a
// later command runs.
var wrappers = map[string]bool{
	".": true, "alias": true, "bash": true, "builtin": true, "busybox": true, "chroot": true, "chrt": true,
	"command": true, "dash": true, "doas": true, "enable": true, "env": true, "eval": true, "exec": true,
	"expect": true, "faketime": true, "firejail": true, "fish": true, "flock": true, "hash": true,
	"ionice": true, "ksh": true, "ltrace": true, "mapfile": true, "nice": true, "nohup": true,
	"nsenter": true, "numactl": true, "parallel": true, "pkexec": true, "prlimit": true,
	"proxychains": true, "proxychains4": true, "read": true, "readarray": true, "rlwrap": true,
	"runuser": true, "screen": true, "script": true, "setpriv": true, "setsid": true, "sg": true,
	"sh": true, "source": true, "sshpass": true, "stdbuf": true, "strace": true, "su": true,
	"sudo": true, "systemd-run": true, "taskset": true, "time": true, "timeout": true, "tmux": true,
	"torsocks": true, "trap": true, "unbuffer": true, "unshare": true, "valgrind": true, "watch": true,
	"xargs": true, "zsh": true,
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
	"cargo":     {names: []string{"config"}},
	"npm":       {names: []string{"script-shell", "node-options"}},
	"pnpm":      {names: []string{"script-shell", "node-options"}},
	"wget":      {letters: "O"},
	"make":      {letters: "E"},
	"printf":    {letters: "v"},
}

// maxLineBytes bounds the lines ParseLine parses: the parser recurses once per
// nesting level, and deep enough nesting overflows the goroutine stack.
const maxLineBytes = 16 << 10

// ParseLine parses a bash command line. Only plain syntax is safe: simple
// commands, lists, pipes, groups, if, while and for over plain words; anything
// else is risky. A line that does not parse keeps its whitespace-separated words
// as a single command, so deny rules still see them; so does one over
// maxLineBytes, which is not parsed at all.
func ParseLine(text string) Line {
	if len(text) > maxLineBytes {
		return Line{Commands: [][]string{strings.Fields(text)}, Risky: true}
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		return Line{Commands: [][]string{strings.Fields(text)}, Risky: true}
	}
	var line Line
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case nil, *syntax.File, *syntax.Stmt, *syntax.Comment, *syntax.BinaryCmd, *syntax.Subshell, *syntax.Block,
			*syntax.IfClause, *syntax.WhileClause, *syntax.ForClause, *syntax.WordIter, *syntax.Word, *syntax.Lit:
		case *syntax.CallExpr:
			line.addCall(node)
		case *syntax.SglQuoted:
			line.Risky = line.Risky || node.Dollar
		case *syntax.DblQuoted:
			line.Risky = line.Risky || node.Dollar
		case *syntax.Redirect:
			line.Risky = line.Risky || writesFile(node)
		default:
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
	for i, arg := range call.Args {
		word, ok := literal(arg)
		if !ok || expands(arg, word, i == 0) {
			l.Risky = true
		}
		words = append(words, word)
	}
	if !plainCommand(call.Args[0]) || wrappers[filepath.Base(words[0])] || runsProgram(words) {
		l.Risky = true
	}
	l.Commands = append(l.Commands, words)
}

// plainCommand reports whether a command name is unquoted text without escapes,
// so rules compare it exactly as bash looks it up.
func plainCommand(word *syntax.Word) bool {
	if len(word.Parts) != 1 {
		return false
	}
	lit, ok := word.Parts[0].(*syntax.Lit)
	return ok && lit.Value != "" && !strings.Contains(lit.Value, `\`)
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

// expands reports whether bash may turn a word into other words: a brace
// expansion, a glob in a command name, or a glob whose matches may start with "-".
func expands(word *syntax.Word, text string, command bool) bool {
	probe := *word
	if syntax.SplitBraces(&probe) && slices.ContainsFunc(probe.Parts, func(part syntax.WordPart) bool {
		_, brace := part.(*syntax.BraceExp)
		return brace
	}) {
		return true
	}
	for i, part := range word.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		at := globAt(lit.Value)
		if at < 0 {
			continue
		}
		before, _ := literal(&syntax.Word{Parts: word.Parts[:i]})
		if command || strings.HasPrefix(text, "-") || before == "" && at == 0 {
			return true
		}
	}
	return false
}

// globAt is the offset of the first unescaped glob character in unquoted text,
// or -1; "[" counts only with a "]" after it.
func globAt(value string) int {
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '*', '?':
			return i
		case '[':
			if strings.Contains(value[i+1:], "]") {
				return i
			}
		}
	}
	return -1
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
			if part.Dollar {
				return "", false
			}
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
