// Package permission decides whether a tool call runs, asks first or is blocked:
// rules matched against the call's subject, bash command-line parsing and the
// presets behind the permission modes.
package permission

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
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
	// calls limits a preset rule to calls by what they do; stored rules match any.
	calls callFilter
}

// callFilter is which calls a preset rule matches by whether they change anything.
type callFilter uint8

const (
	anyCall callFilter = iota
	changingCalls
	readOnlyCalls
)

// String is the rule as users read it, such as "bash: go test:*".
func (r Rule) String() string {
	return Suggestion{Tool: r.Tool, Pattern: r.Pattern}.String()
}

func (r Rule) wildcard() bool {
	pattern := strings.TrimSpace(r.Pattern)
	return pattern == "" || pattern == "*"
}

// covers reports whether the rule applies to req: its tool, a tool whose rules
// cover it, or every tool.
func (r Rule) covers(req Request) bool {
	switch {
	case r.calls == changingCalls && req.ReadOnly, r.calls == readOnlyCalls && !req.ReadOnly:
		return false
	}
	return r.Tool == "*" || req.names(r.Tool)
}

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

// Request is one tool call as rules see it; Tool is the name rules are written
// for and CoveredBy the other tools whose rules also apply, such as read for
// the searches, which read what they find. ReadOnly marks a call that changes
// nothing.
type Request struct {
	Tool      string
	CoveredBy []string
	Subject   Subject
	ReadOnly  bool
}

func (req Request) names(tool string) bool {
	return strings.EqualFold(tool, req.Tool) || slices.ContainsFunc(req.CoveredBy, func(name string) bool { return strings.EqualFold(tool, name) })
}

// Verdict is how rules treat one call and the rule that decided it; the zero
// Verdict means no rule matched and the tool's own default applies.
type Verdict struct {
	Effect Effect
	Rule   Rule
}

// Evaluate applies rules to req: deny rules first (the preset's too), then ask
// rules, then allow rules; preset allow rules are consulted only when none of
// those decided. A risky command line asks whenever a deny or ask rule may
// catch its tool.
func Evaluate(req Request, rules []Rule, preset []Rule) Verdict {
	var line Line
	if req.Subject.Kind == KindCommand {
		line = ParseLine(req.Subject.Value)
	}
	for _, effect := range []Effect{Deny, Ask} {
		for _, rule := range slices.Concat(rules, preset) {
			if rule.Effect == effect && rule.covers(req) && rule.touches(req.Subject, line) {
				return Verdict{Effect: effect, Rule: rule}
			}
		}
	}
	if line.Risky {
		if rule, ok := guardRule(rules, req.Tool); ok {
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

// EvaluateEach applies rules to req once for each of the subjects one call
// reaches: any deny decides, then any ask; the call is allowed only when every
// subject is. With no subjects it is Evaluate of req.
func EvaluateEach(req Request, subjects []Subject, rules []Rule, preset []Rule) Verdict {
	if len(subjects) == 0 {
		return Evaluate(req, rules, preset)
	}
	var allowed, asked Verdict
	undecided := false
	for _, subject := range subjects {
		req.Subject = subject
		switch verdict := Evaluate(req, rules, preset); verdict.Effect {
		case Deny:
			return verdict
		case Ask:
			asked = cmp.Or(asked, verdict)
		case Allow:
			allowed = cmp.Or(allowed, verdict)
		default:
			undecided = true
		}
	}
	switch {
	case asked.Effect != "":
		return asked
	case undecided:
		return Verdict{}
	}
	return allowed
}

// touches reports whether the rule matches the subject or, for a command line,
// any of its simple commands. A search matches by its directory; what it finds
// below meets the rules through SearchSkip.
func (r Rule) touches(subject Subject, line Line) bool {
	if r.wildcard() {
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

// SearchSkip is what a search from req's directory leaves out: the paths a
// deny or ask rule for it matches. Rules that match the directory itself are
// left aside, since the call as a whole already met them. nil when no rule can
// leave anything out.
func SearchSkip(req Request, rules []Rule) func(path string) bool {
	if req.Subject.Kind != KindDirectory {
		return nil
	}
	var guards []string
	for _, rule := range rules {
		if (rule.Effect == Deny || rule.Effect == Ask) && rule.covers(req) && !rule.wildcard() && !matchSubject(rule.Pattern, req.Subject) {
			guards = append(guards, rule.Pattern)
		}
	}
	if len(guards) == 0 {
		return nil
	}
	return func(path string) bool {
		file := Subject{Kind: KindFile, Value: path}
		return slices.ContainsFunc(guards, func(pattern string) bool { return matchSubject(pattern, file) })
	}
}

// guardRule finds a deny or ask rule that may catch some calls of tool: one for the
// tool itself or one with a pattern for every tool.
func guardRule(rules []Rule, tool string) (Rule, bool) {
	for _, rule := range rules {
		if (rule.Effect == Deny || rule.Effect == Ask) && (strings.EqualFold(rule.Tool, tool) || rule.Tool == "*" && !rule.wildcard()) {
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
		if rule.Effect != Allow || !rule.covers(req) {
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
		rule, ok := commandAllowedBy(rules, req, words)
		if !ok {
			return Rule{}, false
		}
		if i == 0 {
			first = rule
		}
	}
	return first, true
}

func commandAllowedBy(rules []Rule, req Request, words []string) (Rule, bool) {
	for _, rule := range rules {
		if rule.Effect == Allow && rule.covers(req) && matchCommand(rule.Pattern, words, false) {
			return rule, true
		}
	}
	return Rule{}, false
}

// SuggestEach is Suggest for a call that reaches several subjects; ok only
// when every subject suggests the same rule.
func SuggestEach(req Request, subjects []Subject, root string) (Suggestion, bool) {
	if len(subjects) == 0 {
		return Suggest(req, root)
	}
	var first Suggestion
	for i, subject := range subjects {
		req.Subject = subject
		suggestion, ok := Suggest(req, root)
		if !ok || i > 0 && suggestion != first {
			return Suggestion{}, false
		}
		first = suggestion
	}
	return first, true
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
// exactSubcommands are those whose next words change too much for a prefix.
// Runners are risky and get no suggestion. Every other command, interpreters
// included, is suggested as its exact line.
var (
	subcommandTools = map[string]bool{
		"apt": true, "brew": true, "bundle": true, "cargo": true, "composer": true, "docker": true,
		"dotnet": true, "gh": true, "git": true, "go": true, "gradle": true, "helm": true, "just": true,
		"kubectl": true, "make": true, "mvn": true, "npm": true, "pip": true, "pip3": true, "pnpm": true,
		"podman": true, "poetry": true, "rustup": true, "systemctl": true, "terraform": true, "uv": true, "yarn": true,
	}
	exactSubcommands = map[string]bool{"docker compose": true, "podman compose": true}
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
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
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
// default, accept_edits also allows file edits inside the working directory,
// full_auto allows every call no rule denies or asks for, and plan denies every
// call that may change something while read-only bash runs without asking.
// Every mode also carries Builtin.
const (
	ModeDefault     = "default"
	ModeAcceptEdits = "accept_edits"
	ModeFullAuto    = "full_auto"
	ModePlan        = "plan"
)

// PlanRuleID is the ID of plan mode's deny rule, so a blocked call can say why.
const PlanRuleID = "preset_plan"

// Builtin are the allow rules of every mode, which no stored rule removes: a
// session's ask or deny rule still wins over them. Listing memory never asks,
// while its changes do.
var Builtin = []Rule{{ID: "builtin_memory_list", Tool: "memory", Pattern: "list", Effect: Allow}}

// Preset is the rules mode adds after a session's own rules, Builtin included;
// root is the working directory with symlinks resolved.
func Preset(mode string, root string) []Rule {
	preset := slices.Clone(Builtin)
	switch mode {
	case ModeFullAuto:
		return append(preset, Rule{Tool: "*", Effect: Allow})
	case ModeAcceptEdits:
		if root == "" {
			return preset
		}
		pattern := strings.TrimSuffix(root, "/") + "/**"
		return append(preset,
			Rule{Tool: "write", Pattern: pattern, Effect: Allow},
			Rule{Tool: "edit", Pattern: pattern, Effect: Allow},
		)
	case ModePlan:
		return append(preset,
			Rule{ID: PlanRuleID, Tool: "*", Effect: Deny, calls: changingCalls},
			Rule{Tool: "bash", Effect: Allow, calls: readOnlyCalls},
		)
	default:
		return preset
	}
}
