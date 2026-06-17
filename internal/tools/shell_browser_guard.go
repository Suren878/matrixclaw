package tools

import (
	"path/filepath"
	"strings"
)

const managedBrowserSetupMessage = "Managed Browser setup is only available through Modules -> Browser -> Install/Repair."

func blockedManagedBrowserInstallCommand(command string) bool {
	normalizedCommand := normalizeManagedBrowserCommandText(command)
	for _, marker := range []string{
		"runtime/browser/playwright-mcp",
		"runtime/browser/ms-playwright",
	} {
		if strings.Contains(normalizedCommand, marker) {
			return true
		}
	}

	tokens := shellCommandTokens(command)
	for i, token := range tokens {
		switch normalizeBrowserInstallToken(token) {
		case "npm":
			if npmExecInstallsManagedBrowser(tokens[i+1:]) {
				return true
			}
		case "npx":
			if commandArgsInstallManagedBrowser(tokens[i+1:]) {
				return true
			}
		case "playwright-mcp":
			if containsNormalizedBrowserToken(tokens[i+1:], "install-browser") {
				return true
			}
		}
	}
	return false
}

func normalizeManagedBrowserCommandText(command string) string {
	command = strings.ToLower(strings.ReplaceAll(command, "\\", "/"))
	command = strings.ReplaceAll(command, "${matrixclaw_runtime_dir}", "runtime")
	command = strings.ReplaceAll(command, "$matrixclaw_runtime_dir", "runtime")
	return command
}

func npmExecInstallsManagedBrowser(tokens []string) bool {
	for i, token := range tokens {
		switch normalizeBrowserInstallToken(token) {
		case "exec", "x":
			return commandArgsInstallManagedBrowser(tokens[i+1:])
		}
	}
	return false
}

func commandArgsInstallManagedBrowser(tokens []string) bool {
	if !containsNormalizedBrowserToken(tokens, "install-browser") {
		return false
	}
	return containsPlaywrightMCPPackage(tokens) || containsNormalizedBrowserToken(tokens, "playwright-mcp")
}

func containsPlaywrightMCPPackage(tokens []string) bool {
	for _, token := range tokens {
		token = strings.ToLower(strings.Trim(strings.TrimSpace(token), "'\""))
		if token == "@playwright/mcp" || strings.HasPrefix(token, "@playwright/mcp@") {
			return true
		}
	}
	return false
}

func containsNormalizedBrowserToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if normalizeBrowserInstallToken(token) == want {
			return true
		}
	}
	return false
}

func normalizeBrowserInstallToken(token string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	token = strings.Trim(token, "'\"")
	token = strings.ReplaceAll(token, "\\", "/")
	if token == "" {
		return ""
	}
	base := filepath.Base(token)
	base = strings.TrimSuffix(base, ".cmd")
	if strings.HasPrefix(base, "playwright-mcp@") {
		return "playwright-mcp"
	}
	return strings.TrimSpace(base)
}

func shellCommandTokens(command string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n', '\r', ';', '&', '|', '(', ')':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}
