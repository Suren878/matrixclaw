package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var out strings.Builder
	out.WriteString("^")
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				out.WriteString(".*")
				i++
			} else {
				out.WriteString(`[^/]*`)
			}
		case '?':
			out.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			out.WriteByte('\\')
			out.WriteByte(pattern[i])
		default:
			out.WriteByte(pattern[i])
		}
	}
	out.WriteString("$")
	return regexp.Compile(out.String())
}

func shouldSkipHidden(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func matchesAnyIgnore(path string, root string, ignore []string) bool {
	if len(ignore) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, pattern := range ignore {
		regex, err := globToRegexp(pattern)
		if err != nil {
			continue
		}
		if regex.MatchString(rel) {
			return true
		}
	}
	return false
}

var errStopWalk = fmt.Errorf("stop walk")

// walkErrorUnlessRoot lets a search go past an entry it cannot read (macOS
// TCC folders, lost+found); only an unreadable root fails the call.
func walkErrorUnlessRoot(path string, root string, err error) error {
	if path == root {
		return err
	}
	return nil
}
