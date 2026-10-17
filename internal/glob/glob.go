// Package glob matches slash-separated paths against glob patterns: "*" and
// "?" stay within one path element, "**" as a whole element matches any number
// of elements (none included), "[...]" is a character class ("[!...]" negated)
// and "{a,b}" lists alternatives. Every MatrixClaw path pattern goes through it.
package glob

import (
	"regexp"
	"slices"
	"strings"
	"sync"
)

// maxAlternatives bounds brace expansion; a pattern that would expand further
// keeps its braces as literal text.
const maxAlternatives = 1024

// Pattern is a compiled glob.
type Pattern struct {
	alternatives [][]element
}

type element struct {
	globstar bool
	// kind says how literal is compared when re is nil.
	kind    matchKind
	literal string
	re      *regexp.Regexp
	// dot is set when the element starts with a literal dot, so it names hidden entries.
	dot bool
}

type matchKind int

const (
	exact matchKind = iota
	prefix
	suffix
	anything
)

// Compile compiles pattern. It never fails: an unclosed bracket or brace and a
// class Go cannot express are literal text.
func Compile(pattern string) *Pattern {
	alternatives := expandBraces(pattern)
	if len(alternatives) > maxAlternatives {
		alternatives = []string{pattern}
	}
	p := &Pattern{alternatives: make([][]element, 0, len(alternatives))}
	for _, alternative := range alternatives {
		var elems []element
		for _, part := range strings.Split(alternative, "/") {
			elem := compileElement(part)
			if elem.globstar && len(elems) > 0 && elems[len(elems)-1].globstar {
				continue
			}
			elems = append(elems, elem)
		}
		p.alternatives = append(p.alternatives, elems)
	}
	return p
}

// CompileFilter compiles a filter pattern the way ripgrep and .gitignore read
// one: a pattern with no slash matches a name at any depth.
func CompileFilter(pattern string) *Pattern {
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	return Compile(pattern)
}

var cache sync.Map

// Match reports whether path matches pattern, hidden names included; compiled
// patterns are cached, so pattern should come from a small set such as rules.
func Match(pattern string, path string) bool {
	if p, ok := cache.Load(pattern); ok {
		return p.(*Pattern).Match(path)
	}
	p := Compile(pattern)
	cache.Store(pattern, p)
	return p.Match(path)
}

// Match reports whether path matches; wildcards match hidden names too.
func (p *Pattern) Match(path string) bool {
	return p.match(strings.Split(path, "/"), false)
}

// MatchVisible is Match where wildcards skip hidden names (a leading dot), so
// only an element that starts with a dot itself, such as ".github", names one.
func (p *Pattern) MatchVisible(path string) bool {
	return p.match(strings.Split(path, "/"), true)
}

// Reaches reports whether a path below dir may match visibly, so a walk can
// skip a directory that cannot hold a match.
func (p *Pattern) Reaches(dir string) bool {
	names := strings.Split(dir, "/")
	for _, elems := range p.alternatives {
		if reaches(elems, names) {
			return true
		}
	}
	return false
}

func (p *Pattern) match(names []string, visible bool) bool {
	for _, elems := range p.alternatives {
		if matchElements(elems, names, visible) {
			return true
		}
	}
	return false
}

func matchElements(elems []element, names []string, visible bool) bool {
	for len(elems) > 0 {
		if elems[0].globstar {
			if fixed := elems[1:]; !slices.ContainsFunc(fixed, func(e element) bool { return e.globstar }) {
				skip := len(names) - len(fixed)
				return skip >= 0 && (!visible || !slices.ContainsFunc(names[:skip], hidden)) && matchElements(fixed, names[skip:], visible)
			}
			for i := 0; i <= len(names); i++ {
				if matchElements(elems[1:], names[i:], visible) {
					return true
				}
				if i < len(names) && visible && hidden(names[i]) {
					return false
				}
			}
			return false
		}
		if len(names) == 0 || !elems[0].matches(names[0], visible) {
			return false
		}
		elems, names = elems[1:], names[1:]
	}
	return len(names) == 0
}

func reaches(elems []element, names []string) bool {
	for len(names) > 0 {
		if len(elems) == 0 {
			return false
		}
		if elems[0].globstar {
			for i := 0; i <= len(names); i++ {
				if i == len(names) || reaches(elems[1:], names[i:]) {
					return true
				}
				if hidden(names[i]) {
					return false
				}
			}
			return false
		}
		if !elems[0].matches(names[0], true) {
			return false
		}
		elems, names = elems[1:], names[1:]
	}
	return len(elems) > 0
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func (e element) matches(name string, visible bool) bool {
	if visible && hidden(name) && !e.dot {
		return false
	}
	switch {
	case e.re != nil:
		return e.re.MatchString(name)
	case e.kind == prefix:
		return strings.HasPrefix(name, e.literal)
	case e.kind == suffix:
		return strings.HasSuffix(name, e.literal)
	case e.kind == anything:
		return true
	}
	return name == e.literal
}

func compileElement(part string) element {
	if part == "**" {
		return element{globstar: true}
	}
	elem := element{dot: strings.HasPrefix(part, "."), literal: part}
	if !strings.ContainsAny(part, `*?[\`) {
		return elem
	}
	if body, ok := strings.CutPrefix(part, "*"); ok && !strings.ContainsAny(body, `*?[\`) {
		elem.kind, elem.literal = suffix, body
		if body == "" {
			elem.kind = anything
		}
		return elem
	}
	if body, ok := strings.CutSuffix(part, "*"); ok && !strings.ContainsAny(body, `*?[\`) {
		elem.kind, elem.literal = prefix, body
		return elem
	}
	var expr strings.Builder
	expr.WriteString(`(?s)^`)
	for i := 0; i < len(part); i++ {
		switch c := part[i]; c {
		case '*':
			expr.WriteString(`.*`)
		case '?':
			expr.WriteString(`.`)
		case '\\':
			if i+1 < len(part) {
				i++
			}
			expr.WriteString(regexp.QuoteMeta(part[i : i+1]))
		case '[':
			end := classEnd(part, i)
			if end < 0 {
				expr.WriteString(`\[`)
				continue
			}
			expr.WriteString(classExpr(part[i+1 : end]))
			i = end
		default:
			expr.WriteString(regexp.QuoteMeta(part[i : i+1]))
		}
	}
	expr.WriteString(`$`)
	re, err := regexp.Compile(expr.String())
	if err != nil {
		return elem
	}
	elem.re = re
	return elem
}

// classEnd is the index of the "]" closing the class opened at start, or -1.
func classEnd(s string, start int) int {
	i := start + 1
	if i < len(s) && (s[i] == '!' || s[i] == '^') {
		i++
	}
	if i < len(s) && s[i] == ']' {
		i++
	}
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case ']':
			return i
		}
	}
	return -1
}

// classExpr turns the body of a glob class into a regexp class.
func classExpr(body string) string {
	var expr strings.Builder
	expr.WriteByte('[')
	if body != "" && (body[0] == '!' || body[0] == '^') {
		expr.WriteByte('^')
		body = body[1:]
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '\\' && i+1 < len(body) {
			i++
			c = body[i]
		}
		if strings.IndexByte(`\[]^`, c) >= 0 {
			expr.WriteByte('\\')
		}
		expr.WriteByte(c)
	}
	expr.WriteByte(']')
	return expr.String()
}

// expandBraces lists the alternatives of every "{a,b}" in pattern; braces
// without a match stay literal.
func expandBraces(pattern string) []string {
	depth, open := 0, -1
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '[':
			if end := classEnd(pattern, i); end > 0 {
				i = end
			}
		case '{':
			if depth == 0 {
				open = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			if depth--; depth > 0 {
				continue
			}
			prefix, suffix := pattern[:open], pattern[i+1:]
			var out []string
			for _, alternative := range splitAlternatives(pattern[open+1 : i]) {
				out = append(out, expandBraces(prefix+alternative+suffix)...)
				if len(out) > maxAlternatives {
					return out
				}
			}
			return out
		}
	}
	return []string{pattern}
}

// splitAlternatives splits a brace body at its top-level commas.
func splitAlternatives(body string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

// Base is the leading directories of pattern that hold no wildcard, such as
// "src/app" for "src/app/**/*.ts"; a walk for the pattern can start there.
func Base(pattern string) string {
	parts := strings.Split(pattern, "/")
	n := 0
	for n < len(parts)-1 && parts[n] != "" && !strings.ContainsAny(parts[n], `*?[{\`) {
		n++
	}
	return strings.Join(parts[:n], "/")
}
