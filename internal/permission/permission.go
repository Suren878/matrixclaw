// Package permission decides whether a tool call runs, asks first or is blocked:
// rules matched against the call's subject, bash command-line parsing and the
// presets behind the permission modes.
package permission

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Effect is what a rule does with the calls it matches.
type Effect string

const (
	Allow Effect = "allow"
	Ask   Effect = "ask"
	Deny  Effect = "deny"
)

// Valid reports whether e is a known effect.
func (e Effect) Valid() bool {
	return e == Allow || e == Ask || e == Deny
}

// Scope is where a rule applies: its session and that session's subagents, or
// every session.
type Scope string

const (
	ScopeSession Scope = "session"
	ScopeGlobal  Scope = "global"
)

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool {
	return s == ScopeSession || s == ScopeGlobal
}

// Rule matches the calls of Tool whose subject matches Pattern. Tool "*" is every
// tool; an empty Pattern or "*" is every call of the tool.
type Rule struct {
	ID        string    `json:"id"`
	Tool      string    `json:"tool"`
	Pattern   string    `json:"pattern,omitempty"`
	Effect    Effect    `json:"effect"`
	Scope     Scope     `json:"scope"`
	SessionID string    `json:"session_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// String is the rule as users read it, such as "bash: go test:*".
func (r Rule) String() string {
	return Suggestion{Tool: r.Tool, Pattern: r.Pattern}.String()
}

func (r Rule) wildcard() bool {
	pattern := strings.TrimSpace(r.Pattern)
	return pattern == "" || pattern == "*"
}

// covers reports whether the rule applies to tool; read rules also cover the
// searches, which read what they find.
func (r Rule) covers(tool string) bool {
	return r.Tool == "*" || strings.EqualFold(r.Tool, tool) || strings.EqualFold(r.Tool, "read") && searchTools[strings.ToLower(tool)]
}

var searchTools = map[string]bool{"grep": true, "glob": true, "ls": true}

// Suggestion is the rule an "Always allow" answer saves.
type Suggestion struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern,omitempty"`
}

func (s Suggestion) String() string {
	if pattern := strings.TrimSpace(s.Pattern); pattern != "" && pattern != "*" {
		return s.Tool + ": " + pattern
	}
	return s.Tool
}

// Kind says how rules match a subject.
type Kind string

const (
	KindFile      Kind = "file"
	KindDirectory Kind = "directory"
	KindCommand   Kind = "command"
	KindDomain    Kind = "domain"
	KindName      Kind = "name"
)

// Subject is what rules match of one call: an absolute file or directory path, a
// bash command line, a host name or an MCP "server__tool" name. The zero Subject
// is matched only by rules for the whole tool.
type Subject struct {
	Kind  Kind
	Value string
}

// Request is one tool call as rules see it; Tool is the name rules are written for.
type Request struct {
	Tool    string
	Subject Subject
}

// Verdict is how rules treat one call and the rule that decided it; the zero
// Verdict means no rule matched and the tool's own default applies.
type Verdict struct {
	Effect Effect
	Rule   Rule
}

// Evaluate applies rules to req: deny rules first, then ask rules, then allow
// rules; preset rules are consulted only when none of those decided. A risky
// command line asks whenever a deny or ask rule names its tool.
func Evaluate(req Request, rules []Rule, preset []Rule) Verdict {
	var line Line
	if req.Subject.Kind == KindCommand {
		line = ParseLine(req.Subject.Value)
	}
	for _, effect := range []Effect{Deny, Ask} {
		for _, rule := range rules {
			if rule.Effect == effect && rule.covers(req.Tool) && rule.touches(req.Subject, line) {
				return Verdict{Effect: effect, Rule: rule}
			}
		}
	}
	if line.Risky {
		if rule, ok := guardedBy(rules, req.Tool); ok {
			return Verdict{Effect: Ask, Rule: rule}
		}
	}
	for _, set := range [][]Rule{rules, preset} {
		if rule, ok := allowedBy(set, req, line); ok {
			return Verdict{Effect: Allow, Rule: rule}
		}
	}
	return Verdict{}
}

// touches reports whether the rule matches the subject, any path a search from
// a directory subject may reach or, for a command line, any of its simple commands.
func (r Rule) touches(subject Subject, line Line) bool {
	if r.wildcard() {
		return true
	}
	if subject.Kind == KindDirectory && reaches(r.Pattern, subject.Value) {
		return true
	}
	if subject.Kind != KindCommand {
		return matchSubject(r.Pattern, subject)
	}
	for _, words := range line.Commands {
		if matchCommand(r.Pattern, words, true) {
			return true
		}
	}
	return false
}

// guardedBy finds a deny or ask rule written for tool itself.
func guardedBy(rules []Rule, tool string) (Rule, bool) {
	for _, rule := range rules {
		if (rule.Effect == Deny || rule.Effect == Ask) && strings.EqualFold(rule.Tool, tool) {
			return rule, true
		}
	}
	return Rule{}, false
}

// allowedBy finds the allow rule that lets req run. A command line needs a rule
// for the whole tool, or no risky construct and an allow rule for every one of
// its simple commands.
func allowedBy(rules []Rule, req Request, line Line) (Rule, bool) {
	for _, rule := range rules {
		if rule.Effect != Allow || !rule.covers(req.Tool) {
			continue
		}
		if rule.wildcard() || req.Subject.Kind != KindCommand && matchSubject(rule.Pattern, req.Subject) {
			return rule, true
		}
	}
	if req.Subject.Kind != KindCommand || line.Risky || len(line.Commands) == 0 {
		return Rule{}, false
	}
	var first Rule
	for i, words := range line.Commands {
		rule, ok := commandAllowedBy(rules, req.Tool, words)
		if !ok {
			return Rule{}, false
		}
		if i == 0 {
			first = rule
		}
	}
	return first, true
}

func commandAllowedBy(rules []Rule, tool string, words []string) (Rule, bool) {
	for _, rule := range rules {
		if rule.Effect == Allow && rule.covers(tool) && matchCommand(rule.Pattern, words, false) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Suggest proposes the rule an "Always allow" answer saves for req. A command
// with a known subcommand gets its prefix ("go test:*"), any other command its
// exact line; a file or search inside root its directory, one outside the exact
// path; a domain or MCP tool itself. ok is false for a risky or compound line.
func Suggest(req Request, root string) (Suggestion, bool) {
	suggestion := Suggestion{Tool: req.Tool}
	value := req.Subject.Value
	switch req.Subject.Kind {
	case KindCommand:
		pattern, ok := suggestCommand(value)
		if !ok {
			return Suggestion{}, false
		}
		suggestion.Pattern = pattern
	case KindFile:
		suggestion.Pattern = suggestPath(filepath.Dir(value), value, root)
	case KindDirectory:
		suggestion.Pattern = suggestPath(value, value, root)
	case KindDomain, KindName:
		suggestion.Pattern = value
	}
	return suggestion, true
}

// subcommandTools are the commands whose second word names a subcommand;
// exactSubcommands are those that run programs or code they are given. Every
// other command, interpreters included, is suggested as its exact line.
var (
	subcommandTools = map[string]bool{
		"apt": true, "brew": true, "bundle": true, "cargo": true, "composer": true, "docker": true,
		"dotnet": true, "gh": true, "git": true, "go": true, "gradle": true, "helm": true, "just": true,
		"kubectl": true, "make": true, "mvn": true, "npm": true, "pip": true, "pip3": true, "pnpm": true,
		"podman": true, "poetry": true, "rustup": true, "systemctl": true, "terraform": true, "uv": true, "yarn": true,
	}
	exactSubcommands = map[string]bool{
		"bundle exec": true, "cargo run": true, "docker exec": true, "docker run": true, "go run": true,
		"kubectl exec": true, "npm exec": true, "pnpm dlx": true, "pnpm exec": true, "podman exec": true,
		"podman run": true, "poetry run": true, "uv run": true, "yarn dlx": true, "yarn exec": true,
	}
)

// suggestCommand is a prefix for a command with a known subcommand and the exact
// line otherwise; ok is false when no pattern can name the line.
func suggestCommand(value string) (string, bool) {
	line := ParseLine(value)
	if line.Risky || len(line.Commands) != 1 {
		return "", false
	}
	words := line.Commands[0]
	for _, word := range words {
		if word == "" || strings.ContainsFunc(word, unicode.IsSpace) {
			return "", false
		}
	}
	name := words[0]
	if len(words) > 1 && subcommandTools[name] && subcommand(words[1]) && !exactSubcommands[name+" "+words[1]] {
		return name + " " + words[1] + ":*", true
	}
	if strings.HasSuffix(words[len(words)-1], ":*") {
		return "", false
	}
	return strings.Join(words, " "), true
}

// suggestPath is dir/** when dir is inside root and neither "/" nor the home
// directory, and exact otherwise.
func suggestPath(dir string, exact string, root string) string {
	root = strings.TrimSuffix(root, "/")
	inside := root != "" && (dir == root || strings.HasPrefix(dir, root+"/"))
	if !inside || dir == "/" || dir == homeDir() {
		return exact
	}
	return strings.TrimSuffix(dir, "/") + "/**"
}

// subcommand reports whether a word can name a subcommand, as in "go test" or
// "git status", rather than a flag, a path or a file.
func subcommand(word string) bool {
	if word == "" || word[0] == '-' {
		return false
	}
	for _, r := range word {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		return real
	}
	return home
}

// Mode names are the permission presets: default leaves every tool to its own
// default, accept_edits also allows file edits inside the working directory and
// full_auto allows every call no rule denies or asks for.
const (
	ModeDefault     = "default"
	ModeAcceptEdits = "accept_edits"
	ModeFullAuto    = "full_auto"
)

// Preset is the rules mode adds after a session's own rules; root is the working
// directory with symlinks resolved.
func Preset(mode string, root string) []Rule {
	switch mode {
	case ModeFullAuto:
		return []Rule{{Tool: "*", Effect: Allow}}
	case ModeAcceptEdits:
		if root == "" {
			return nil
		}
		pattern := strings.TrimSuffix(root, "/") + "/**"
		return []Rule{
			{Tool: "write", Pattern: pattern, Effect: Allow},
			{Tool: "edit", Pattern: pattern, Effect: Allow},
		}
	default:
		return nil
	}
}
