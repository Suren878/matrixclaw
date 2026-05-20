package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (s *Service) buildConfig(draft Draft) (Config, error) {
	httpAddr := strings.TrimSpace(draft.HTTPAddr)
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr()
	}

	dbPath := strings.TrimSpace(draft.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath()
	}
	timezone := strings.TrimSpace(draft.Timezone)
	if timezone == "" {
		timezone = defaultTimezone()
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Config{}, fmt.Errorf("invalid daemon timezone %q", timezone)
	}

	autostart := ParseBool(draft.AutostartOnBoot)
	telegramEnabled := ParseBool(draft.TelegramEnabled)
	telegramToken := strings.TrimSpace(draft.TelegramBotToken)
	telegramUserID := strings.TrimSpace(draft.TelegramAllowedUID)
	telegramProviderSetup := ParseBool(draft.TelegramProviderSetup)
	if telegramEnabled {
		if telegramToken == "" {
			return Config{}, errors.New("telegram bot token is required when Telegram is enabled")
		}
		if telegramUserID == "" {
			return Config{}, errors.New("telegram allowed user id is required when Telegram is enabled")
		}
		if _, err := strconv.ParseInt(telegramUserID, 10, 64); err != nil {
			return Config{}, errors.New("telegram allowed user id must be numeric")
		}
	}

	existing, err := s.Load()
	switch {
	case err == nil:
	case errors.Is(err, ErrConfigNotFound), errors.Is(err, ErrUnsupportedConfigVersion):
		existing = Config{}
	default:
		return Config{}, err
	}

	configured := make([]ProviderConfig, 0, len(draft.Providers))
	seen := make(map[string]struct{}, len(draft.Providers))
	for _, providerDraft := range draft.Providers {
		if !ProviderDraftConfigured(providerDraft) {
			continue
		}
		provider, err := s.buildProviderConfig(providerDraft, existing)
		if err != nil {
			return Config{}, err
		}
		if _, exists := seen[provider.ID]; exists {
			return Config{}, fmt.Errorf("duplicate provider %q", provider.Name)
		}
		seen[provider.ID] = struct{}{}
		configured = append(configured, provider)
	}

	activeProviderID := providers.NormalizeProviderID(draft.ActiveProviderID)
	if activeProviderID == "" && len(configured) > 0 {
		activeProviderID = configured[0].ID
	}
	if activeProviderID != "" {
		foundActive := false
		for _, provider := range configured {
			if sameProvider(provider.ID, activeProviderID) {
				activeProviderID = provider.ID
				foundActive = true
				break
			}
		}
		if !foundActive {
			return Config{}, errors.New("active provider is not configured")
		}
	}

	apiToken, err := existingOrNewAPIToken(existing.Daemon.APIToken)
	if err != nil {
		return Config{}, fmt.Errorf("generate api token: %w", err)
	}
	cfg := Config{
		Version:          CurrentVersion,
		CompletedAt:      s.now().UTC(),
		ActiveProviderID: activeProviderID,
		Assistant: AssistantConfig{
			Name:               strings.TrimSpace(draft.AssistantName),
			SystemPrompt:       strings.TrimSpace(draft.AssistantSystemPrompt),
			CustomInstructions: strings.TrimSpace(draft.AssistantCustomPrompt),
		},
		Providers: configured,
		Daemon: DaemonConfig{
			HTTPAddr:        httpAddr,
			DBPath:          dbPath,
			Timezone:        timezone,
			APIToken:        apiToken,
			AutostartOnBoot: autostart,
		},
		Clients: ClientsConfig{
			Terminal: TerminalConfig{Enabled: true},
			Telegram: TelegramConfig{
				Enabled:            telegramEnabled,
				BotToken:           telegramToken,
				AllowedUserID:      telegramUserID,
				AllowProviderSetup: telegramProviderSetup,
			},
		},
		Modules: existing.Modules,
	}
	return normalizeConfig(cfg), nil
}

func defaultDraft() Draft {
	return Draft{
		AssistantName:         "matrixclaw",
		AssistantSystemPrompt: DefaultAssistantSystemPrompt(),
		HTTPAddr:              defaultHTTPAddr(),
		DBPath:                defaultDBPath(),
		Timezone:              defaultTimezone(),
		AutostartOnBoot:       "no",
		TelegramEnabled:       "no",
		TelegramProviderSetup: "no",
	}
}

func (s *Service) buildValidatedConfig(ctx context.Context, draft Draft) (Config, TelegramSummary, error) {
	cfg, err := s.buildConfig(draft)
	if err != nil {
		return Config{}, TelegramSummary{}, err
	}
	telegramSummary, err := s.validateRuntime(ctx, cfg)
	if err != nil {
		return Config{}, TelegramSummary{}, err
	}
	return cfg, telegramSummary, nil
}

func summaryWithRuntimeValidation(cfg Config, telegramSummary TelegramSummary) Summary {
	summary := SummaryFromConfig(cfg)
	if cfg.Clients.Telegram.Enabled {
		summary.Telegram = telegramSummary
	}
	return summary
}

func (s *Service) EnsureDaemonAPIToken() (Config, error) {
	cfg, err := s.Load()
	if err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.Daemon.APIToken) != "" {
		return cfg, nil
	}
	token, err := generateAPIToken()
	if err != nil {
		return Config{}, fmt.Errorf("generate api token: %w", err)
	}
	cfg.Daemon.APIToken = token
	if err := s.store.Save(cfg); err != nil {
		return Config{}, err
	}
	return s.Load()
}

func existingOrNewAPIToken(existing string) (string, error) {
	if token := strings.TrimSpace(existing); token != "" {
		return token, nil
	}
	return generateAPIToken()
}

func generateAPIToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *Service) validateRuntime(ctx context.Context, cfg Config) (TelegramSummary, error) {
	if s.telegramValidate == nil || !cfg.Clients.Telegram.Enabled {
		return TelegramSummary{}, nil
	}
	return s.telegramValidate.Validate(ctx, cfg.Clients.Telegram.BotToken)
}

func (s *Service) buildProviderConfig(draft ProviderDraft, existing Config) (ProviderConfig, error) {
	providerID := providers.NormalizeProviderID(draft.ID)
	if providerID == "" {
		return ProviderConfig{}, errors.New("provider id is required")
	}

	var (
		option    ProviderOption
		hasOption bool
	)
	catalogID := providers.NormalizeProviderID(draft.CatalogID)
	if catalogID == "" {
		catalogID = providerID
	}
	if option, hasOption = lookupProviderOption(catalogID); hasOption && !option.Implemented {
		return ProviderConfig{}, fmt.Errorf("provider %q is listed but not implemented yet", option.Name)
	}

	providerType := providers.NormalizeOptionalProviderType(draft.Type)
	providerName := strings.TrimSpace(draft.Name)
	apiKeyEnv := strings.TrimSpace(draft.APIKeyEnv)
	if hasOption {
		providerID = option.ID
		catalogID = option.ID
		providerType = option.Type
		providerName = option.Name
		apiKeyEnv = option.APIKeyEnv
	} else {
		switch providerType {
		case providers.TypeOpenAICompat, providers.TypeOpenAICodex, providers.TypeAnthropic:
		default:
			return ProviderConfig{}, fmt.Errorf("unsupported provider type %q", providerType)
		}
		if providerName == "" {
			return ProviderConfig{}, errors.New("provider name is required")
		}
		if apiKeyEnv == "" {
			if providerType == providers.TypeAnthropic {
				apiKeyEnv = "ANTHROPIC_API_KEY"
			} else {
				apiKeyEnv = "OPENAI_COMPAT_API_KEY"
			}
		}
		catalogID = ""
	}

	apiKey := normalizeProviderAPIKey(draft.APIKey)
	if apiKey == "" && draft.HasStoredAPIKey {
		if stored, ok := findProviderConfig(existing, providerID); ok {
			apiKey = normalizeProviderAPIKey(stored.APIKey)
		}
	}
	if apiKey == "" && providerType != providers.TypeOpenAICodex && normalizeProviderAPIKey(providerAPIKeyFromEnvName(apiKeyEnv)) == "" {
		return ProviderConfig{}, fmt.Errorf("%s API key is required", providerDisplayName(draft, option, hasOption))
	}

	baseURL := strings.TrimSpace(draft.BaseURL)
	if hasOption && baseURL == "" {
		baseURL = option.DefaultBaseURL
	}
	if baseURL == "" {
		return ProviderConfig{}, fmt.Errorf("%s base URL is required", providerDisplayName(draft, option, hasOption))
	}

	model := strings.TrimSpace(draft.Model)
	if hasOption && model == "" {
		model = option.DefaultModel
	}
	model = providers.NormalizeModelID(catalogID, providerType, model)
	if model == "" {
		return ProviderConfig{}, fmt.Errorf("%s model is required", providerDisplayName(draft, option, hasOption))
	}

	reasoningEffort := providers.NormalizeReasoningEffortForModel(catalogID, providerType, model, draft.ReasoningEffort)

	maxOutputTokens := int64(0)
	if value := strings.TrimSpace(draft.MaxOutputTokens); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return ProviderConfig{}, errors.New("max output tokens must be a positive integer")
		}
		maxOutputTokens = parsed
	}

	toolUseMode := providers.NormalizeOptionalToolUseMode(draft.ToolUseMode)
	if !providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
		ProviderID:   catalogID,
		ProviderType: providerType,
		ModelID:      model,
	}).ProviderCapabilities.ToolCalling {
		toolUseMode = ""
	}

	return ProviderConfig{
		ID:              providerID,
		CatalogID:       catalogID,
		Name:            providerName,
		Type:            providerType,
		APIKey:          apiKey,
		APIKeyEnv:       apiKeyEnv,
		BaseURL:         baseURL,
		Model:           model,
		ToolUseMode:     toolUseMode,
		MaxOutputTokens: maxOutputTokens,
		ReasoningEffort: reasoningEffort,
	}, nil
}

func draftFromConfig(cfg Config) Draft {
	cfg = normalizeConfig(cfg)
	draft := Draft{
		ActiveProviderID:      cfg.ActiveProviderID,
		AssistantName:         cfg.Assistant.Name,
		AssistantSystemPrompt: cfg.Assistant.SystemPrompt,
		AssistantCustomPrompt: cfg.Assistant.CustomInstructions,
		Providers:             make([]ProviderDraft, 0, len(cfg.Providers)),
		HTTPAddr:              cfg.Daemon.HTTPAddr,
		DBPath:                cfg.Daemon.DBPath,
		Timezone:              cfg.Daemon.Timezone,
		AutostartOnBoot:       BoolString(cfg.Daemon.AutostartOnBoot),
		TelegramEnabled:       BoolString(cfg.Clients.Telegram.Enabled),
		TelegramBotToken:      cfg.Clients.Telegram.BotToken,
		TelegramAllowedUID:    cfg.Clients.Telegram.AllowedUserID,
		TelegramProviderSetup: BoolString(cfg.Clients.Telegram.AllowProviderSetup),
	}
	for _, provider := range cfg.Providers {
		draft.Providers = append(draft.Providers, draftProviderFromConfig(provider))
	}
	return draft
}

func defaultHTTPAddr() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_HTTP_ADDR")); value != "" {
		return value
	}
	return firstAvailableLoopbackHTTPAddr()
}

func defaultDBPath() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_DB_PATH")); value != "" {
		return value
	}

	return filepath.Join(defaultStateDir(), "matrixclaw", "matrixclaw.db")
}

func DefaultDBPath() string {
	return defaultDBPath()
}

func defaultStateDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state")
	}
	return os.TempDir()
}

func defaultTimezone() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_TIMEZONE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("TZ")); value != "" {
		return value
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}
	return "UTC"
}

func BoolString(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func ParseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func sameProvider(left string, right string) bool {
	return providers.NormalizeProviderID(left) == providers.NormalizeProviderID(right)
}

func providerDisplayName(draft ProviderDraft, option ProviderOption, hasOption bool) string {
	if hasOption {
		return option.Name
	}
	if name := strings.TrimSpace(draft.Name); name != "" {
		return name
	}
	return "Provider"
}
