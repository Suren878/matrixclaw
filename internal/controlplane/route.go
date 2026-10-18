package controlplane

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
)

// A command line is "/<command> <verb words> <operands> <value>". Verb words
// are fixed; each operand is one token with "%", spaces and control
// characters percent-escaped ("%" alone is an empty operand); the value is
// the free text after the operands: typed with the command or a prompt's
// answer. A verb that takes operands never starts a longer verb, so an
// operand (an MCP server named "add") is never read as a verb.

// Verb is one route of a command: the words naming it after the command,
// the operands that follow them and whether free text may follow those.
type Verb struct {
	cmd   CommandID
	words string
	args  int
	value bool
}

var verbs = map[CommandID][]Verb{}

// verb declares a route of command taking args operands and no value.
func verb(command CommandID, words string, args int) Verb {
	return register(Verb{cmd: command, words: words, args: args})
}

// valueVerb declares a route whose operands may be followed by free text.
func valueVerb(command CommandID, words string, args int) Verb {
	return register(Verb{cmd: command, words: words, args: args, value: true})
}

func register(v Verb) Verb {
	if v.words != strings.ToLower(strings.Join(strings.Fields(v.words), " ")) || (v.words == "" && v.args > 0) {
		panic(fmt.Sprintf("controlplane: malformed verb %q of %s", v.words, v.cmd))
	}
	for _, other := range verbs[v.cmd] {
		if other.words == v.words ||
			(other.args > 0 && strings.HasPrefix(v.words, other.words+" ")) ||
			(v.args > 0 && strings.HasPrefix(other.words, v.words+" ")) {
			panic(fmt.Sprintf("controlplane: verb %q of %s clashes with %q", v.words, v.cmd, other.words))
		}
	}
	verbs[v.cmd] = append(verbs[v.cmd], v)
	return v
}

// Route is a verb with its operands and value.
type Route struct {
	Verb  Verb
	Args  []string
	Value string
}

// command is the command line that runs v with args.
func (v Verb) command(args ...string) string {
	return Route{Verb: v, Args: args}.Encode()
}

// commandValue is the command line that runs v with args and value.
func (v Verb) commandValue(value string, args ...string) string {
	return Route{Verb: v, Args: args, Value: value}.Encode()
}

// Command is the command r runs a verb of.
func (r Route) Command() CommandID {
	return r.Verb.cmd
}

// Is reports whether r runs verb.
func (r Route) Is(verb Verb) bool {
	return r.Verb == verb
}

// arg is operand i, "" when it was not given.
func (r Route) arg(i int) string {
	if i < len(r.Args) {
		return r.Args[i]
	}
	return ""
}

// Encode is r's command line; DecodeRoute reads it back.
func (r Route) Encode() string {
	parts := []string{CommandText(r.Verb.cmd)}
	if r.Verb.words != "" {
		parts = append(parts, r.Verb.words)
	}
	value := strings.TrimSpace(r.Value)
	args := make([]string, r.Verb.args)
	copy(args, r.Args)
	if value == "" {
		for len(args) > 0 && args[len(args)-1] == "" {
			args = args[:len(args)-1]
		}
	}
	for _, arg := range args {
		parts = append(parts, escapeArg(arg))
	}
	if value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}

// DecodeRoute reads a command line: the longest verb its words name (one that
// takes a value when text is left over), its operands and the rest as value.
// It is false for text that is no command.
func DecodeRoute(line string) (Route, bool) {
	spec, rest, ok := Parse(line)
	if !ok {
		return Route{}, false
	}
	tokens := splitTokens(rest)
	candidates := verbs[spec.ID]
	if !slices.ContainsFunc(candidates, func(v Verb) bool { return v.words == "" }) {
		candidates = append([]Verb{{cmd: spec.ID}}, candidates...)
	}
	var best Route
	bestWords, bestFits := -1, false
	for _, v := range candidates {
		words := strings.Fields(v.words)
		if len(words) > len(tokens) || !slices.EqualFunc(words, tokens[:len(words)], func(word string, t token) bool {
			return strings.EqualFold(word, t.text)
		}) {
			continue
		}
		route := Route{Verb: v, Args: make([]string, v.args)}
		end := len(words)
		for i := range v.args {
			if end < len(tokens) {
				route.Args[i] = unescapeArg(tokens[end].text)
				end++
			}
		}
		if end > 0 {
			route.Value = strings.TrimSpace(rest[tokens[end-1].end:])
		} else {
			route.Value = strings.TrimSpace(rest)
		}
		fits := route.Value == "" || v.value
		if (fits && !bestFits) || (fits == bestFits && len(words) > bestWords) {
			best, bestWords, bestFits = route, len(words), fits
		}
	}
	return best, true
}

type token struct {
	text string
	end  int // byte offset just past the token
}

func splitTokens(text string) []token {
	var tokens []token
	start := -1
	for i, r := range text {
		switch {
		case unicode.IsSpace(r) && start >= 0:
			tokens = append(tokens, token{text: text[start:i], end: i})
			start = -1
		case !unicode.IsSpace(r) && start < 0:
			start = i
		}
	}
	if start >= 0 {
		tokens = append(tokens, token{text: text[start:], end: len(text)})
	}
	return tokens
}

func escapeArg(arg string) string {
	if arg == "" {
		return "%"
	}
	var b strings.Builder
	for _, r := range arg {
		if r == '%' || unicode.IsSpace(r) || unicode.IsControl(r) {
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// unescapeArg reads an operand; a typed one that is no valid escape stays
// as typed.
func unescapeArg(text string) string {
	if text == "%" {
		return ""
	}
	decoded, err := url.PathUnescape(text)
	if err != nil {
		return text
	}
	return decoded
}

// CommandText is the command's slash word, such as "/modules".
func CommandText(id CommandID) string {
	for _, spec := range commandCatalog {
		if spec.ID == id {
			return spec.Command
		}
	}
	return "/" + string(id)
}
