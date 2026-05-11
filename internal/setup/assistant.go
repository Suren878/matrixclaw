package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
)

const projectContextTitle = "Project context:"

type AssistantPromptContext struct {
	HTTPAddr              string
	DBPath                string
	Timezone              string
	AutostartOnBoot       bool
	TelegramEnabled       bool
	TelegramProviderSetup bool
	ActiveProviderID      string
	ConfiguredProviders   int
}

func InitializeAssistantSystemPrompt(current string) string {
	return InitializeAssistantSystemPromptWithContext(current, AssistantPromptContext{})
}

func InitializeAssistantSystemPromptForDraft(current string, draft Draft) string {
	return InitializeAssistantSystemPromptWithContext(current, AssistantPromptContext{
		HTTPAddr:              draft.HTTPAddr,
		DBPath:                draft.DBPath,
		Timezone:              draft.Timezone,
		AutostartOnBoot:       ParseBool(draft.AutostartOnBoot),
		TelegramEnabled:       ParseBool(draft.TelegramEnabled),
		TelegramProviderSetup: ParseBool(draft.TelegramProviderSetup),
		ActiveProviderID:      draft.ActiveProviderID,
		ConfiguredProviders:   len(ConfiguredProviders(draft)),
	})
}

func InitializeAssistantSystemPromptForConfig(current string, cfg Config) string {
	return InitializeAssistantSystemPromptWithContext(current, AssistantPromptContext{
		HTTPAddr:              cfg.Daemon.HTTPAddr,
		DBPath:                cfg.Daemon.DBPath,
		Timezone:              cfg.Daemon.Timezone,
		AutostartOnBoot:       cfg.Daemon.AutostartOnBoot,
		TelegramEnabled:       cfg.Clients.Telegram.Enabled,
		TelegramProviderSetup: cfg.Clients.Telegram.AllowProviderSetup,
		ActiveProviderID:      cfg.ActiveProviderID,
		ConfiguredProviders:   len(cfg.Providers),
	})
}

func InitializeAssistantSystemPromptWithContext(current string, promptContext AssistantPromptContext) string {
	base := strings.TrimSpace(current)
	if base == "" {
		base = DefaultAssistantSystemPrompt()
	}
	context := compactProjectContext(promptContext)
	if context == "" {
		return base
	}
	if idx := strings.Index(base, projectContextTitle); idx >= 0 {
		base = strings.TrimSpace(base[:idx])
	}
	return strings.TrimSpace(base + "\n\n" + projectContextTitle + "\n" + context)
}

func compactProjectContext(promptContext AssistantPromptContext) string {
	root := resolveProjectRoot()
	if strings.TrimSpace(root) == "" {
		return ""
	}
	if gitRoot := gitOutput(root, "rev-parse", "--show-toplevel"); gitRoot != "" {
		root = gitRoot
	}
	parts := []string{"project_root=" + filepath.Clean(root)}
	if setupPath, err := DefaultConfigPath(); err == nil && strings.TrimSpace(setupPath) != "" {
		parts = append(parts, "setup_config="+filepath.Clean(setupPath))
	}
	if branch := gitOutput(root, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		parts = append(parts, "git_branch="+branch)
	}
	if remote := gitOutput(root, "remote", "get-url", "origin"); remote != "" {
		parts = append(parts, "git_remote="+remote)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		parts = append(parts, "language=Go", "test=go test ./...")
	}
	if httpAddr := strings.TrimSpace(promptContext.HTTPAddr); httpAddr != "" {
		parts = append(parts, "daemon_http="+httpAddr)
	}
	if dbPath := strings.TrimSpace(promptContext.DBPath); dbPath != "" {
		parts = append(parts, "sqlite_db="+filepath.Clean(dbPath))
	}
	if timezone := strings.TrimSpace(promptContext.Timezone); timezone != "" {
		parts = append(parts, "daemon_timezone="+timezone)
	}
	if promptContext.ConfiguredProviders > 0 {
		parts = append(parts, fmt.Sprintf("providers_configured=%d", promptContext.ConfiguredProviders))
	}
	if activeProviderID := strings.TrimSpace(promptContext.ActiveProviderID); activeProviderID != "" {
		parts = append(parts, "active_provider="+activeProviderID)
	}
	parts = append(parts,
		"automation=enabled",
		"automation_schedules=once,interval,cron",
		"automation_tools=create_reminder,create_scheduled_ai_task",
		"autostart="+enabledLabel(promptContext.AutostartOnBoot),
		"telegram="+enabledLabel(promptContext.TelegramEnabled),
		"telegram_provider_setup="+enabledLabel(promptContext.TelegramProviderSetup),
	)
	if commands := compactCommandContext(); commands != "" {
		parts = append(parts, "commands="+commands)
	}
	return "- " + strings.Join(parts, "\n- ")
}

func resolveProjectRoot() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_PROJECT_ROOT")); value != "" {
		return filepath.Clean(value)
	}
	root, err := os.Getwd()
	if err == nil && strings.TrimSpace(root) != "" {
		if gitRoot := gitOutput(root, "rev-parse", "--show-toplevel"); gitRoot != "" {
			return filepath.Clean(gitRoot)
		}
		if hasGoModule(root) {
			return filepath.Clean(root)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "projects", "matrixclaw")
		if hasGoModule(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return root
}

func hasGoModule(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	return err == nil && strings.Contains(string(data), "module github.com/Suren878/matrixclaw")
}

func compactCommandContext() string {
	items := make([]string, 0, len(commandcatalog.Catalog())+4)
	for _, spec := range commandcatalog.Catalog() {
		command := strings.TrimSpace(spec.Command)
		if command == "" {
			continue
		}
		items = append(items, command+" ("+strings.TrimSpace(spec.Description)+")")
	}
	items = append(items,
		"/sessions -> create/select/rename/delete sessions",
		"/remind -> create one-time reminders",
		"/tasks -> list/create/pause/resume/delete scheduled AI tasks",
		"/provider key <provider> <key> -> configure provider API key",
		"/model <model> -> switch current session model",
		"/restart -> full daemon service restart",
	)
	return strings.Join(items, "; ")
}

func gitOutput(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%s", out))
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
