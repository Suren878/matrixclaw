package permission

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/idna"
)

// matchSubject matches a pattern against a path, domain or name subject; domains
// compare normalised.
func matchSubject(pattern string, subject Subject) bool {
	if subject.Value == "" {
		return false
	}
	switch subject.Kind {
	case KindFile, KindDirectory:
		if base, ok := strings.CutSuffix(pattern, "/**"); ok && subject.Value == base {
			return true
		}
		return globMatch(pattern, subject.Value)
	case KindDomain:
		return globMatch(NormalizeDomain(pattern), NormalizeDomain(subject.Value))
	default:
		return globMatch(pattern, subject.Value)
	}
}

// reaches reports whether a path pattern may match dir or a path below it,
// comparing element by element; an element with "**" may match anything below.
func reaches(pattern string, dir string) bool {
	want := strings.Split(strings.TrimSuffix(pattern, "/"), "/")
	have := strings.Split(strings.TrimSuffix(dir, "/"), "/")
	for i, elem := range have {
		switch {
		case i == len(want):
			return false
		case strings.Contains(want[i], "**"):
			return true
		case !globMatch(want[i], elem):
			return false
		}
	}
	return true
}

// matchCommand matches one simple command: "go test:*" matches the commands that
// start with those words, "git status" only that exact command. byName compares
// the command by its base name, so "rm" also matches /bin/rm.
func matchCommand(pattern string, words []string, byName bool) bool {
	body, prefix := strings.CutSuffix(strings.TrimSpace(pattern), ":*")
	want := strings.Fields(body)
	if len(want) == 0 || len(words) < len(want) || !prefix && len(words) != len(want) {
		return false
	}
	for i, word := range want {
		got := words[i]
		if i == 0 && byName {
			got, word = filepath.Base(got), filepath.Base(word)
		}
		if got != word {
			return false
		}
	}
	return true
}

// NormalizeDomain is a host name or domain pattern as rules compare it: lower
// case, without a trailing dot, and with international labels in punycode.
func NormalizeDomain(domain string) string {
	labels := strings.Split(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), "."), ".")
	for i, label := range labels {
		if ascii, err := idna.Lookup.ToASCII(label); err == nil && !strings.Contains(label, "*") {
			labels[i] = ascii
		}
	}
	return strings.Join(labels, ".")
}

// globs caches compiled patterns; they come from rules, so the set stays small.
var globs sync.Map

// globMatch matches a glob where "*" and "?" stay within one path element and
// "**" crosses elements ("**/" also matches no element at all).
func globMatch(pattern string, value string) bool {
	if re, ok := globs.Load(pattern); ok {
		return re.(*regexp.Regexp).MatchString(value)
	}
	re, err := compileGlob(pattern)
	if err != nil {
		return false
	}
	globs.Store(pattern, re)
	return re.MatchString(value)
}

func compileGlob(pattern string) (*regexp.Regexp, error) {
	var expr strings.Builder
	expr.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '*' && i+1 < len(runes) && runes[i+1] == '*':
			i++
			if i+1 < len(runes) && runes[i+1] == '/' {
				i++
				expr.WriteString("(?:.*/)?")
			} else {
				expr.WriteString(".*")
			}
		case runes[i] == '*':
			expr.WriteString("[^/]*")
		case runes[i] == '?':
			expr.WriteString("[^/]")
		default:
			expr.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	expr.WriteString("$")
	return regexp.Compile(expr.String())
}
