package setup

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const CurrentVersion = 3

type Config struct {
	Version          int              `json:"version"`
	CompletedAt      time.Time        `json:"completed_at"`
	ActiveProviderID string           `json:"active_provider_id,omitempty"`
	Assistant        AssistantConfig  `json:"assistant,omitempty"`
	Providers        []ProviderConfig `json:"providers,omitempty"`
	Daemon           DaemonConfig     `json:"daemon"`
	Clients          ClientsConfig    `json:"clients"`
	Modules          ModulesConfig    `json:"modules,omitempty"`
}

type AssistantConfig struct {
	Name               string `json:"name,omitempty"`
	SystemPrompt       string `json:"system_prompt,omitempty"`
	CustomInstructions string `json:"custom_instructions,omitempty"`
}

type ProviderConfig struct {
	ID              string                `json:"id"`
	CatalogID       string                `json:"catalog_id,omitempty"`
	Name            string                `json:"name"`
	Type            string                `json:"type"`
	APIKey          string                `json:"api_key"`
	APIKeyEnv       string                `json:"api_key_env,omitempty"`
	BaseURL         string                `json:"base_url,omitempty"`
	Model           string                `json:"model"`
	MaxOutputTokens int64                 `json:"max_output_tokens,omitempty"`
	ReasoningEffort string                `json:"reasoning_effort,omitempty"`
	ToolUseMode     providers.ToolUseMode `json:"tool_use_mode,omitempty"`
}

type DaemonConfig struct {
	HTTPAddr        string `json:"http_addr"`
	DBPath          string `json:"db_path"`
	Timezone        string `json:"timezone,omitempty"`
	APIToken        string `json:"api_token,omitempty"`
	AutostartOnBoot bool   `json:"autostart_on_boot"`
}

type ClientsConfig struct {
	Terminal TerminalConfig `json:"terminal"`
	Telegram TelegramConfig `json:"telegram"`
}

type TerminalConfig struct {
	Enabled bool `json:"enabled"`
}

type TelegramConfig struct {
	Enabled            bool   `json:"enabled"`
	BotToken           string `json:"bot_token,omitempty"`
	AllowedUserID      string `json:"allowed_user_id,omitempty"`
	AllowProviderSetup bool   `json:"allow_provider_setup,omitempty"`
}

type ModulesConfig struct {
	ExternalAgents map[string]ExternalAgentConfig `json:"external_agents,omitempty"`
}

type ExternalAgentConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Path    string `json:"path,omitempty"`
}

type Draft struct {
	ActiveProviderID      string
	AssistantName         string
	AssistantSystemPrompt string
	AssistantCustomPrompt string
	Providers             []ProviderDraft
	HTTPAddr              string
	DBPath                string
	Timezone              string
	AutostartOnBoot       string
	TelegramEnabled       string
	TelegramBotToken      string
	TelegramAllowedUID    string
	TelegramProviderSetup string
}

type ProviderDraft struct {
	ID                  string
	CatalogID           string
	Name                string
	Type                string
	APIKey              string
	APIKeyEnv           string
	BaseURL             string
	Model               string
	ToolUseMode         providers.ToolUseMode
	MaxOutputTokens     string
	ReasoningEffort     string
	HasStoredAPIKey     bool
	StoredAPIKeyPreview string
}

type ProviderOption struct {
	ID              string
	Name            string
	Type            string
	Implemented     bool
	RequiresBaseURL bool
	Capabilities    providers.Capabilities
	DefaultBaseURL  string
	BaseURLOptions  []providers.BaseURLOption
	DefaultModel    string
	APIKeyEnv       string
	Notes           string
}

type ProviderSetupItem struct {
	ID              string                    `json:"id"`
	CatalogID       string                    `json:"catalog_id,omitempty"`
	Name            string                    `json:"name"`
	Type            string                    `json:"type"`
	Status          string                    `json:"status"`
	Configured      bool                      `json:"configured"`
	Active          bool                      `json:"active"`
	Implemented     bool                      `json:"implemented"`
	RequiresBaseURL bool                      `json:"requires_base_url,omitempty"`
	Capabilities    providers.Capabilities    `json:"capabilities,omitempty"`
	BaseURL         string                    `json:"base_url,omitempty"`
	BaseURLOptions  []providers.BaseURLOption `json:"base_url_options,omitempty"`
	Model           string                    `json:"model,omitempty"`
	ReasoningEffort string                    `json:"reasoning_effort,omitempty"`
	ToolUseMode     providers.ToolUseMode     `json:"tool_use_mode,omitempty"`
	DefaultModel    string                    `json:"default_model,omitempty"`
	APIKeyPreview   string                    `json:"api_key_preview,omitempty"`
	Notes           string                    `json:"notes,omitempty"`
}

type ProviderSetupUpdate struct {
	Name            string                `json:"name,omitempty"`
	Type            string                `json:"type,omitempty"`
	APIKey          string                `json:"api_key,omitempty"`
	BaseURL         string                `json:"base_url,omitempty"`
	Model           string                `json:"model,omitempty"`
	ReasoningEffort string                `json:"reasoning_effort,omitempty"`
	ToolUseMode     providers.ToolUseMode `json:"tool_use_mode,omitempty"`
	Active          bool                  `json:"active,omitempty"`
}

type ProviderSetupListResponse struct {
	Providers []ProviderSetupItem `json:"providers"`
}

type ProviderSetupResponse struct {
	Provider ProviderSetupItem `json:"provider"`
}

type ProviderModelsResponse struct {
	Models []string `json:"models"`
}

type ProviderSetupOKResponse struct {
	OK bool `json:"ok"`
}

type ApplyResult struct {
	Config   Config
	Path     string
	Summary  Summary
	Warnings []string
}

type Summary struct {
	Assistant AssistantSummary
	Provider  ProviderSummary
	Daemon    DaemonSummary
	Telegram  TelegramSummary
}

type AssistantSummary struct {
	Name   string
	Status string
}

type ProviderSummary struct {
	ID            string
	Name          string
	Model         string
	Status        string
	APIKeyPreview string
}

type DaemonSummary struct {
	Status        string
	HTTPAddr      string
	DBPath        string
	Timezone      string
	Autostart     bool
	RuntimeStatus string
	Installed     bool
	Running       bool
	Enabled       bool
	Warning       string
}

type TelegramSummary struct {
	Status   string
	Username string
	Warning  string
}
