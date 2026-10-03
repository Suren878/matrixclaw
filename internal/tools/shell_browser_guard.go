package tools

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
)

const managedBrowserSetupMessage = "Managed Browser setup is only available through Modules -> Browser -> Install/Repair."

// managedBrowserRuntime matches the managed browser's runtime directories,
// written out or through $MATRIXCLAW_RUNTIME_DIR.
var managedBrowserRuntime = regexp.MustCompile(`runtime(_dir\}?)?/browser/(playwright-mcp|ms-playwright)`)

// installsManagedBrowser reports whether a command line touches the managed
// browser runtime or installs a browser through the Playwright MCP package;
// the Browser module owns both.
func installsManagedBrowser(command string) bool {
	if managedBrowserRuntime.MatchString(strings.ToLower(strings.ReplaceAll(command, `\`, "/"))) {
		return true
	}
	for _, words := range permission.ParseLine(command).Commands {
		if slices.Contains(words, "install-browser") && slices.ContainsFunc(words, playwrightMCP) {
			return true
		}
	}
	return false
}

// playwrightMCP reports whether a word names the Playwright MCP package or its binary.
func playwrightMCP(word string) bool {
	word = strings.ToLower(word)
	base := strings.TrimSuffix(filepath.Base(word), ".cmd")
	return base == "playwright-mcp" || strings.HasPrefix(base, "playwright-mcp@") || word == "@playwright/mcp" || strings.HasPrefix(word, "@playwright/mcp@")
}
