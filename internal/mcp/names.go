package mcp

import (
	"strings"
	"unicode"
)

func ToolID(prefix string, remoteName string) string {
	prefix = SanitizeToolPart(prefix)
	remote := SanitizeToolPart(remoteName)
	if prefix == "" {
		return "mcp_" + remote
	}
	if remote == "" {
		return "mcp_" + prefix
	}
	return "mcp_" + prefix + "_" + remote
}

func SanitizeToolPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
