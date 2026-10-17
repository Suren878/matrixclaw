package glob

import (
	"bufio"
	"bytes"
	"slices"
	"strings"
)

// Ignore is a stack of .gitignore rules; later rules win, as in git. The zero
// Ignore ignores nothing.
type Ignore struct {
	rules []ignoreRule
}

type ignoreRule struct {
	// prefix is the base with a trailing slash; depth counts its elements.
	prefix  string
	depth   int
	pattern *Pattern
	negate  bool
	dirOnly bool
}

// Add returns ig with the rules of a .gitignore file whose patterns are
// relative to base, a slash path; ig itself is unchanged.
func (ig Ignore) Add(base string, content []byte) Ignore {
	rules := slices.Clip(ig.rules)
	base = strings.TrimSuffix(base, "/")
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		if rule, ok := parseIgnoreLine(scanner.Text()); ok {
			rule.prefix, rule.depth = base+"/", strings.Count(base, "/")+1
			rules = append(rules, rule)
		}
	}
	return Ignore{rules: rules}
}

// Ignored reports whether the rules leave out path, a slash path in the same
// space as the bases. Only path itself is checked, not its parents.
func (ig Ignore) Ignored(path string, dir bool) bool {
	var names []string
	for i := len(ig.rules) - 1; i >= 0; i-- {
		rule := ig.rules[i]
		if rule.dirOnly && !dir || !strings.HasPrefix(path, rule.prefix) {
			continue
		}
		if names == nil {
			names = strings.Split(path, "/")
		}
		if rule.pattern.match(names[rule.depth:], false) {
			return !rule.negate
		}
	}
	return false
}

func parseIgnoreLine(line string) (ignoreRule, bool) {
	line = strings.TrimSuffix(line, "\r")
	if trimmed := strings.TrimRight(line, " "); strings.HasSuffix(trimmed, `\`) && len(trimmed) < len(line) {
		line = trimmed + " "
	} else {
		line = trimmed
	}
	if line == "" || line[0] == '#' {
		return ignoreRule{}, false
	}
	var rule ignoreRule
	if line[0] == '!' {
		rule.negate, line = true, line[1:]
	}
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}
	if trimmed, ok := strings.CutSuffix(line, "/"); ok {
		rule.dirOnly, line = true, trimmed
	}
	if line == "" {
		return ignoreRule{}, false
	}
	if strings.Contains(line, "/") {
		line = strings.TrimPrefix(line, "/")
	} else {
		line = "**/" + line
	}
	rule.pattern = Compile(line)
	return rule, true
}
