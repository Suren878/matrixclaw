package permission

import (
	"path/filepath"
	"regexp"
	"strings"
)

// matchSubject matches a pattern against a path, domain or name subject; domains
// compare without case.
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
		return globMatch(strings.ToLower(pattern), strings.ToLower(subject.Value))
	default:
		return globMatch(pattern, subject.Value)
	}
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

// globMatch matches a glob where "*" and "?" stay within one path element and
// "**" crosses elements ("**/" also matches no element at all).
func globMatch(pattern string, value string) bool {
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
	re, err := regexp.Compile(expr.String())
	return err == nil && re.MatchString(value)
}
