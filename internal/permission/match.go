package permission

import (
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/net/idna"

	"github.com/Suren878/matrixclaw/internal/glob"
)

// matchSubject matches a pattern against a path, domain or name subject; domains
// compare normalised.
func matchSubject(pattern string, subject Subject) bool {
	if subject.Value == "" {
		return false
	}
	if subject.Kind == KindDomain {
		return glob.Match(NormalizeDomain(pattern), NormalizeDomain(subject.Value))
	}
	return glob.Match(pattern, subject.Value)
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
