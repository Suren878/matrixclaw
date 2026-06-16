package openaicodex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://chatgpt.com/backend-api/codex"

	oauthClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthIssuer          = "https://auth.openai.com"
	oauthTokenURL        = "https://auth.openai.com/oauth/token"
	codexClientVersion   = "0.130.0"
	refreshSkew          = 120 * time.Second
	defaultRequestTimout = 20 * time.Second
)

type Credentials struct {
	AccessToken  string
	RefreshToken string
	BaseURL      string
	Source       string
	LastRefresh  time.Time
}

type LoginDevice struct {
	UserCode    string
	VerifyURL   string
	Interval    time.Duration
	ExpiresIn   time.Duration
	authID      string
	requestedAt time.Time
}

type LoginResult struct {
	Credentials Credentials
}

type AuthStatus struct {
	SignedIn bool
	Source   string
	Expired  bool
}

type tokenStore struct {
	AuthMode    string            `json:"auth_mode,omitempty"`
	BaseURL     string            `json:"base_url,omitempty"`
	LastRefresh time.Time         `json:"last_refresh,omitempty"`
	Tokens      map[string]string `json:"tokens,omitempty"`
}

func AuthStorePath() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_CODEX_AUTH_FILE")); value != "" {
		return value
	}
	if dir := strings.TrimSpace(os.Getenv("MATRIXCLAW_AUTH_DIR")); dir != "" {
		return filepath.Join(dir, "openai-codex.json")
	}
	return filepath.Join(defaultStateDir(), "matrixclaw", "auth", "openai-codex.json")
}

func ResolveCredentials(ctx context.Context, client *http.Client, baseURL string) (Credentials, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimout}
	}
	if creds, ok, err := readMatrixclawCredentials(); err != nil {
		return Credentials{}, err
	} else if ok {
		creds.BaseURL = firstNonEmpty(baseURL, creds.BaseURL, DefaultBaseURL)
		if tokenExpiring(creds.AccessToken, refreshSkew) {
			refreshed, err := refreshCredentials(ctx, client, creds)
			if err != nil {
				return Credentials{}, err
			}
			if err := saveCredentials(refreshed); err != nil {
				return Credentials{}, err
			}
			return refreshed, nil
		}
		return creds, nil
	}
	if creds, ok := readCodexCLICredentials(); ok {
		if tokenExpiring(creds.AccessToken, 0) {
			return Credentials{}, errors.New("openai-codex: Codex CLI credentials are expired; run `matrixclaw providers login openai-codex`")
		}
		creds.BaseURL = firstNonEmpty(baseURL, creds.BaseURL, DefaultBaseURL)
		return creds, nil
	}
	return Credentials{}, errors.New("openai-codex: no ChatGPT/Codex credentials; run `matrixclaw providers login openai-codex`")
}

func StartDeviceLogin(ctx context.Context, client *http.Client) (LoginDevice, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimout}
	}
	body, _ := json.Marshal(map[string]string{"client_id": oauthClientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthIssuer+"/api/accounts/deviceauth/usercode", bytes.NewReader(body))
	if err != nil {
		return LoginDevice{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return LoginDevice{}, fmt.Errorf("openai-codex: request device code: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return LoginDevice{}, fmt.Errorf("openai-codex: read device code: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return LoginDevice{}, fmt.Errorf("openai-codex: device code request failed: %s", decodeError(raw))
	}
	var payload struct {
		UserCode     string `json:"user_code"`
		DeviceAuthID string `json:"device_auth_id"`
		Interval     int    `json:"interval"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return LoginDevice{}, fmt.Errorf("openai-codex: decode device code: %w", err)
	}
	if strings.TrimSpace(payload.UserCode) == "" || strings.TrimSpace(payload.DeviceAuthID) == "" {
		return LoginDevice{}, errors.New("openai-codex: incomplete device code response")
	}
	interval := time.Duration(payload.Interval) * time.Second
	if interval < 3*time.Second {
		interval = 5 * time.Second
	}
	expires := time.Duration(payload.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	return LoginDevice{
		UserCode:    strings.TrimSpace(payload.UserCode),
		VerifyURL:   oauthIssuer + "/codex/device",
		Interval:    interval,
		ExpiresIn:   expires,
		authID:      strings.TrimSpace(payload.DeviceAuthID),
		requestedAt: time.Now(),
	}, nil
}

func CompleteDeviceLogin(ctx context.Context, client *http.Client, device LoginDevice) (LoginResult, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimout}
	}
	if device.authID == "" || device.UserCode == "" {
		return LoginResult{}, errors.New("openai-codex: incomplete device login")
	}
	deadline := time.Now().Add(device.ExpiresIn)
	if !device.requestedAt.IsZero() {
		deadline = device.requestedAt.Add(device.ExpiresIn)
	}
	var authCode string
	var verifier string
	for {
		if time.Now().After(deadline) {
			return LoginResult{}, errors.New("openai-codex: device login timed out")
		}
		timer := time.NewTimer(device.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return LoginResult{}, ctx.Err()
		case <-timer.C:
		}
		payload := map[string]string{
			"device_auth_id": device.authID,
			"user_code":      device.UserCode,
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthIssuer+"/api/accounts/deviceauth/token", bytes.NewReader(body))
		if err != nil {
			return LoginResult{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return LoginResult{}, fmt.Errorf("openai-codex: poll device login: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			return LoginResult{}, fmt.Errorf("openai-codex: read device poll: %w", readErr)
		}
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound {
			continue
		}
		if res.StatusCode != http.StatusOK {
			return LoginResult{}, fmt.Errorf("openai-codex: device login failed: %s", decodeError(raw))
		}
		var poll struct {
			AuthorizationCode string `json:"authorization_code"`
			CodeVerifier      string `json:"code_verifier"`
		}
		if err := json.Unmarshal(raw, &poll); err != nil {
			return LoginResult{}, fmt.Errorf("openai-codex: decode device poll: %w", err)
		}
		authCode = strings.TrimSpace(poll.AuthorizationCode)
		verifier = strings.TrimSpace(poll.CodeVerifier)
		break
	}
	if authCode == "" || verifier == "" {
		return LoginResult{}, errors.New("openai-codex: device login missing authorization code")
	}
	creds, err := exchangeCode(ctx, client, authCode, verifier)
	if err != nil {
		return LoginResult{}, err
	}
	if err := saveCredentials(creds); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Credentials: creds}, nil
}

func CurrentAuthStatus() AuthStatus {
	if creds, ok, err := readMatrixclawCredentials(); err == nil && ok {
		expired := tokenExpiring(creds.AccessToken, 0)
		return AuthStatus{SignedIn: !expired, Source: firstNonEmpty(creds.Source, "matrixclaw-auth-store"), Expired: expired}
	}
	if creds, ok := readCodexCLICredentials(); ok {
		expired := tokenExpiring(creds.AccessToken, 0)
		return AuthStatus{SignedIn: !expired, Source: firstNonEmpty(creds.Source, "codex-cli-auth-store"), Expired: expired}
	}
	return AuthStatus{}
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
		BaseURL:      firstNonEmpty(store.BaseURL, DefaultBaseURL),
		Source:       "matrixclaw-auth-store",
		LastRefresh:  store.LastRefresh,
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
	return Credentials{
		AccessToken:  access,
		RefreshToken: refresh,
		BaseURL:      DefaultBaseURL,
		Source:       "codex-cli-auth-store",
	}, true
}

func refreshCredentials(ctx context.Context, client *http.Client, creds Credentials) (Credentials, error) {
	body := "grant_type=refresh_token&refresh_token=" + formEscape(creds.RefreshToken) + "&client_id=" + formEscape(oauthClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("openai-codex: refresh token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return Credentials{}, fmt.Errorf("openai-codex: read refresh response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("openai-codex: refresh failed: %s", decodeError(raw))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Credentials{}, fmt.Errorf("openai-codex: decode refresh response: %w", err)
	}
	access, _ := payload["access_token"].(string)
	refresh, _ := payload["refresh_token"].(string)
	access = strings.TrimSpace(access)
	refresh = strings.TrimSpace(refresh)
	if access == "" {
		return Credentials{}, errors.New("openai-codex: refresh response missing access token")
	}
	if refresh == "" {
		refresh = creds.RefreshToken
	}
	return Credentials{
		AccessToken:  access,
		RefreshToken: refresh,
		BaseURL:      firstNonEmpty(creds.BaseURL, DefaultBaseURL),
		Source:       "matrixclaw-auth-store",
		LastRefresh:  time.Now().UTC(),
	}, nil
}

func exchangeCode(ctx context.Context, client *http.Client, code string, verifier string) (Credentials, error) {
	body := "grant_type=authorization_code&code=" + formEscape(code) +
		"&redirect_uri=" + formEscape(oauthIssuer+"/deviceauth/callback") +
		"&client_id=" + formEscape(oauthClientID) +
		"&code_verifier=" + formEscape(verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("openai-codex: exchange device code: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return Credentials{}, fmt.Errorf("openai-codex: read token exchange: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("openai-codex: token exchange failed: %s", decodeError(raw))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Credentials{}, fmt.Errorf("openai-codex: decode token exchange: %w", err)
	}
	access, _ := payload["access_token"].(string)
	refresh, _ := payload["refresh_token"].(string)
	access = strings.TrimSpace(access)
	refresh = strings.TrimSpace(refresh)
	if access == "" || refresh == "" {
		return Credentials{}, errors.New("openai-codex: token exchange response missing tokens")
	}
	return Credentials{
		AccessToken:  access,
		RefreshToken: refresh,
		BaseURL:      DefaultBaseURL,
		Source:       "device-code",
		LastRefresh:  time.Now().UTC(),
	}, nil
}

func EncodeLoginDevice(device LoginDevice) (string, error) {
	if strings.TrimSpace(device.authID) == "" || strings.TrimSpace(device.UserCode) == "" {
		return "", errors.New("openai-codex: incomplete device login")
	}
	payload := struct {
		UserCode    string `json:"user_code"`
		VerifyURL   string `json:"verify_url"`
		IntervalMS  int64  `json:"interval_ms"`
		ExpiresMS   int64  `json:"expires_ms"`
		AuthID      string `json:"auth_id"`
		RequestedAt int64  `json:"requested_at"`
	}{
		UserCode:    strings.TrimSpace(device.UserCode),
		VerifyURL:   strings.TrimSpace(device.VerifyURL),
		IntervalMS:  device.Interval.Milliseconds(),
		ExpiresMS:   device.ExpiresIn.Milliseconds(),
		AuthID:      strings.TrimSpace(device.authID),
		RequestedAt: device.requestedAt.UnixMilli(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeLoginDevice(token string) (LoginDevice, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return LoginDevice{}, fmt.Errorf("openai-codex: decode login session: %w", err)
	}
	var payload struct {
		UserCode    string `json:"user_code"`
		VerifyURL   string `json:"verify_url"`
		IntervalMS  int64  `json:"interval_ms"`
		ExpiresMS   int64  `json:"expires_ms"`
		AuthID      string `json:"auth_id"`
		RequestedAt int64  `json:"requested_at"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return LoginDevice{}, fmt.Errorf("openai-codex: decode login session: %w", err)
	}
	if strings.TrimSpace(payload.UserCode) == "" || strings.TrimSpace(payload.AuthID) == "" {
		return LoginDevice{}, errors.New("openai-codex: incomplete device login")
	}
	interval := time.Duration(payload.IntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expires := time.Duration(payload.ExpiresMS) * time.Millisecond
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	requestedAt := time.Now()
	if payload.RequestedAt > 0 {
		requestedAt = time.UnixMilli(payload.RequestedAt)
	}
	verifyURL := strings.TrimSpace(payload.VerifyURL)
	if verifyURL == "" {
		verifyURL = oauthIssuer + "/codex/device"
	}
	return LoginDevice{
		UserCode:    strings.TrimSpace(payload.UserCode),
		VerifyURL:   verifyURL,
		Interval:    interval,
		ExpiresIn:   expires,
		authID:      strings.TrimSpace(payload.AuthID),
		requestedAt: requestedAt,
	}, nil
}

func saveCredentials(creds Credentials) error {
	path := AuthStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("openai-codex: create auth dir: %w", err)
	}
	store := tokenStore{
		AuthMode:    "chatgpt",
		BaseURL:     firstNonEmpty(creds.BaseURL, DefaultBaseURL),
		LastRefresh: time.Now().UTC(),
		Tokens: map[string]string{
			"access_token":  strings.TrimSpace(creds.AccessToken),
			"refresh_token": strings.TrimSpace(creds.RefreshToken),
		},
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func tokenExpiring(token string, skew time.Duration) bool {
	exp, ok := jwtExpiry(token)
	if !ok {
		return false
	}
	return time.Now().Add(skew).After(exp)
}

func jwtExpiry(token string) (time.Time, bool) {
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if !jwtClaims(token, &claims) || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func jwtChatGPTAccountID(token string) string {
	var claims map[string]any
	if !jwtClaims(token, &claims) {
		return ""
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	accountID, _ := auth["chatgpt_account_id"].(string)
	return strings.TrimSpace(accountID)
}

func jwtClaims(token string, out any) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

func setCodexHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", "codex_cli_rs/0.0.0 (Matrixclaw)")
	req.Header.Set("originator", "codex_cli_rs")
	if accountID := jwtChatGPTAccountID(accessToken); accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}
}

func decodeError(raw []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		if errObj, ok := payload["error"].(map[string]any); ok {
			if message, ok := errObj["message"].(string); ok && strings.TrimSpace(message) != "" {
				return strings.TrimSpace(message)
			}
		}
		if message, ok := payload["error_description"].(string); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
		if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
	}
	if text := strings.TrimSpace(string(raw)); text != "" {
		return text
	}
	return "unknown error"
}

func formEscape(value string) string {
	return url.QueryEscape(value)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
