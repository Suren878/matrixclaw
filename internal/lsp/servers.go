package lsp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Config is setup.json's `diagnostics.lsp`: Disabled turns every language
// server off; Servers replaces the command of a language (an argv such as
// ["pylsp"]), and an empty one turns that language off.
type Config struct {
	Disabled bool                `json:"disabled,omitempty"`
	Servers  map[string][]string `json:"servers,omitempty"`
}

// Validate reports languages that are not built in and blank commands.
func (c Config) Validate() error {
	var problems []error
	for _, lang := range slices.Sorted(maps.Keys(c.Servers)) {
		if languageOf(lang) == nil {
			problems = append(problems, fmt.Errorf("servers key %q is not one of %q", lang, Languages()))
			continue
		}
		if argv := c.Servers[lang]; len(argv) > 0 && strings.TrimSpace(argv[0]) == "" {
			problems = append(problems, fmt.Errorf("servers.%s: the program name is empty", lang))
		}
	}
	return errors.Join(problems...)
}

// language is a built-in language: its files (extension to LSP language id),
// the files that mark its project root, the server commands tried in order
// and the server's settings, sent as initialization options and as the
// configuration section the server asks for.
type language struct {
	name     string
	ids      map[string]string
	markers  []string
	commands [][]string
	section  string
	settings map[string]any
	// settle, when set, returns once the server published the diagnostics
	// of every file the changes to uris affect, so the errors it knows in
	// other files are current; without it only the errors it publishes
	// while the edited files are checked are.
	settle func(ctx context.Context, c *client, uris []string) error
}

var languages = []language{
	{
		name:     "go",
		ids:      map[string]string{".go": "go"},
		markers:  []string{"go.work", "go.mod"},
		commands: [][]string{{"gopls"}},
		// Type errors in the packages that import an edited one come with the
		// deep diagnostics, which gopls delays 1 s by default.
		section:  "gopls",
		settings: map[string]any{"diagnosticsDelay": "100ms"},
		// gopls.diagnose_files diagnoses the whole snapshot, importers of
		// the edited packages included, and returns when it published.
		settle: func(ctx context.Context, c *client, uris []string) error {
			return c.conn.call(ctx, "workspace/executeCommand", map[string]any{
				"command": "gopls.diagnose_files", "arguments": []any{map[string]any{"Files": uris}},
			}, nil)
		},
	},
	{
		name: "typescript",
		ids: map[string]string{".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "typescriptreact",
			".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascriptreact"},
		markers:  []string{"tsconfig.json", "jsconfig.json", "package.json"},
		commands: [][]string{{"typescript-language-server", "--stdio"}},
	},
	{
		name:     "python",
		ids:      map[string]string{".py": "python", ".pyi": "python"},
		markers:  []string{"pyproject.toml", "pyrightconfig.json", "setup.py", "setup.cfg", "requirements.txt", "Pipfile"},
		commands: [][]string{{"pyright-langserver", "--stdio"}, {"pylsp"}},
	},
	{
		name:     "rust",
		ids:      map[string]string{".rs": "rust"},
		markers:  []string{"Cargo.toml"},
		commands: [][]string{{"rust-analyzer"}},
	},
}

// Languages lists the built-in languages by name.
func Languages() []string {
	names := make([]string, 0, len(languages))
	for _, lang := range languages {
		names = append(names, lang.name)
	}
	return names
}

func languageOf(name string) *language {
	for i := range languages {
		if languages[i].name == name {
			return &languages[i]
		}
	}
	return nil
}

// languageFor is the language of a file by its extension; nil for none.
func languageFor(file string) *language {
	ext := strings.ToLower(filepath.Ext(file))
	for i := range languages {
		if _, ok := languages[i].ids[ext]; ok {
			return &languages[i]
		}
	}
	return nil
}

// root is the nearest directory from dir up that holds one of the language's
// markers; "" when there is none.
func (l *language) root(dir string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		for _, marker := range l.markers {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d
			}
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// command is the argv that runs the language's server with its program
// resolved on path; nil when it is off or not installed.
func (l *language) command(cfg Config, path string) []string {
	candidates := l.commands
	if argv, ok := cfg.Servers[l.name]; ok {
		if len(argv) == 0 {
			return nil
		}
		candidates = [][]string{argv}
	}
	for _, argv := range candidates {
		if program := lookPath(argv[0], path); program != "" {
			return append([]string{program}, argv[1:]...)
		}
	}
	return nil
}

// lookPath finds an executable in the directories of path (a PATH value); a
// name with a slash is taken as it is.
func lookPath(name, path string) string {
	executable := func(file string) bool {
		info, err := os.Stat(file)
		return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
	}
	if strings.ContainsRune(name, filepath.Separator) {
		if executable(name) {
			return name
		}
		return ""
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if file := filepath.Join(dir, name); executable(file) {
			return file
		}
	}
	return ""
}

// pathOf is the PATH value of env.
func pathOf(env []string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(env[i], "PATH="); ok {
			return value
		}
	}
	return ""
}
