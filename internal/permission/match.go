package permission

import (
	"path/filepath"
	"regexp"
	"slices"
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
// start with those words, "git status" only that exact command. broad is for
// deny and ask rules: it compares the command by its base name, so "rm" also
// matches /bin/rm, and lets options come before the subcommand, as in "git -C
// dir push".
func matchCommand(pattern string, words []string, broad bool) bool {
	body, prefix := strings.CutSuffix(strings.TrimSpace(pattern), ":*")
	want := strings.Fields(body)
	if len(want) == 0 || len(words) == 0 {
		return false
	}
	name := words[0]
	if broad {
		name, want[0] = filepath.Base(name), filepath.Base(want[0])
	}
	if name != want[0] {
		return false
	}
	for at := 1; ; at++ {
		rest := words[at:]
		if len(rest) >= len(want)-1 && (prefix || len(rest) == len(want)-1) && slices.Equal(rest[:len(want)-1], want[1:]) {
			return true
		}
		if !broad || len(want) == 1 || at == len(words) || !leadingOptions(words[1:at+1]) {
			return false
		}
	}
}

// leadingOptions reports whether words may all be options, each word that does
// not look like one being the value of the option before it.
func leadingOptions(words []string) bool {
	for i, word := range words {
		option := strings.HasPrefix(word, "-") || strings.HasPrefix(word, "+")
		if !option && (i == 0 || !strings.HasPrefix(words[i-1], "-") || strings.Contains(words[i-1], "=")) {
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
