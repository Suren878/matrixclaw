package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/xdg"
)

const (
	DefaultBaseURL = "https://chatgpt.com/backend-api/codex"

	oauthClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthIssuer           = "https://auth.openai.com"
	oauthTokenURL         = "https://auth.openai.com/oauth/token"
	codexClientVersion    = "0.130.0"
	refreshSkew           = 120 * time.Second
	defaultRequestTimeout = 20 * time.Second

	sourceStore    = "matrixclaw-auth-store"
	sourceCodexCLI = "codex-cli-auth-store"
)

type Credentials struct {
	AccessToken  string
	RefreshToken string
	BaseURL      string
	Source       string
}

type AuthStatus struct {
	SignedIn bool
	Source   string
	Expired  bool
}

type tokenStore struct {
	AuthMode string            `json:"auth_mode,omitempty"`
	BaseURL  string            `json:"base_url,omitempty"`
	Tokens   map[string]string `json:"tokens,omitempty"`
}

func AuthStorePath() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_CODEX_AUTH_FILE")); value != "" {
		return value
	}
	if dir := strings.TrimSpace(os.Getenv("MATRIXCLAW_AUTH_DIR")); dir != "" {
		return filepath.Join(dir, "openai-codex.json")
	}
	return filepath.Join(xdg.StateHome(), "matrixclaw", "auth", "openai-codex.json")
}

func CurrentAuthStatus() AuthStatus {
	if creds, ok, err := readMatrixclawCredentials(); err == nil && ok {
		expired := tokenExpiring(creds.AccessToken, 0)
		return AuthStatus{SignedIn: !expired, Source: creds.Source, Expired: expired}
	}
	if creds, ok := readCodexCLICredentials(); ok {
		expired := tokenExpiring(creds.AccessToken, 0)
		return AuthStatus{SignedIn: !expired, Source: creds.Source, Expired: expired}
	}
	return AuthStatus{}
}

// lockAuthStore takes an exclusive flock on the store's lock file. The daemon,
// the CLI and clients share the store, and a refresh token is single use, so
// every read-refresh-write must hold it.
func lockAuthStore(ctx context.Context) (unlock func(), err error) {
	path := AuthStorePath() + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("openai-codex: create auth dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("openai-codex: open auth lock: %w", err)
	}
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = file.Close()
			return nil, fmt.Errorf("openai-codex: lock auth store: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func readMatrixclawCredentials() (Credentials, bool, error) {
	raw, err := os.ReadFile(AuthStorePath())
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, false, nil
	}
	if err != nil {
		return Credentials{}, false, fmt.Errorf("openai-codex: read auth store: %w", err)
	}
	var store tokenStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return Credentials{}, false, fmt.Errorf("openai-codex: decode auth store: %w", err)
	}
	access := strings.TrimSpace(store.Tokens["access_token"])
	refresh := strings.TrimSpace(store.Tokens["refresh_token"])
	if access == "" || refresh == "" {
		return Credentials{}, false, errors.New("openai-codex: auth store is missing tokens")
	}
	return Credentials{
		AccessToken:  access,
		RefreshToken: refresh,
		BaseURL:      textutil.FirstNonEmpty(store.BaseURL, DefaultBaseURL),
		Source:       sourceStore,
	}, true, nil
}

func readCodexCLICredentials() (Credentials, bool) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return Credentials{}, false
		}
		codexHome = filepath.Join(home, ".codex")
	}
	raw, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		return Credentials{}, false
	}
	var payload struct {
		Tokens map[string]string `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Credentials{}, false
	}
	access := strings.TrimSpace(payload.Tokens["access_token"])
	refresh := strings.TrimSpace(payload.Tokens["refresh_token"])
	if access == "" || refresh == "" {
		return Credentials{}, false
	}
	return Credentials{AccessToken: access, RefreshToken: refresh, BaseURL: DefaultBaseURL, Source: sourceCodexCLI}, true
}

// saveCredentials replaces the store atomically; callers hold lockAuthStore.
func saveCredentials(creds Credentials) error {
	path := AuthStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("openai-codex: create auth dir: %w", err)
	}
	store := tokenStore{
		AuthMode: "chatgpt",
		BaseURL:  textutil.FirstNonEmpty(creds.BaseURL, DefaultBaseURL),
		Tokens: map[string]string{
			"access_token":  strings.TrimSpace(creds.AccessToken),
			"refresh_token": strings.TrimSpace(creds.RefreshToken),
		},
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".openai-codex-*.json")
	if err != nil {
		return fmt.Errorf("openai-codex: write auth store: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("openai-codex: write auth store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("openai-codex: write auth store: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("openai-codex: write auth store: %w", err)
	}
	return nil
}
