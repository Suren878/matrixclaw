package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// reauthRequired is an error only `providers login` fixes.
func reauthRequired(reason string) error {
	return fmt.Errorf("openai-codex: %s: %w; run `matrixclaw providers login openai-codex`", reason, providers.ErrReauthRequired)
}

func ResolveCredentials(ctx context.Context, client *http.Client, baseURL string) (Credentials, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	creds, ok, err := readMatrixclawCredentials()
	if err != nil {
		return Credentials{}, err
	}
	if ok && tokenExpiring(creds.AccessToken, refreshSkew) {
		if creds, err = refreshStoredCredentials(ctx, client, ""); err != nil {
			return Credentials{}, err
		}
	}
	if !ok {
		if creds, ok = readCodexCLICredentials(); !ok {
			return Credentials{}, errors.New("openai-codex: no ChatGPT/Codex credentials; run `matrixclaw providers login openai-codex`")
		}
		if tokenExpiring(creds.AccessToken, 0) {
			return Credentials{}, reauthRequired("Codex CLI credentials are expired")
		}
	}
	creds.BaseURL = textutil.FirstNonEmpty(baseURL, creds.BaseURL, DefaultBaseURL)
	return creds, nil
}

// refreshRejected renews credentials the backend answered with 401, once.
func refreshRejected(ctx context.Context, client *http.Client, rejected Credentials) (Credentials, error) {
	if rejected.Source != sourceStore {
		return Credentials{}, reauthRequired("Codex CLI credentials were rejected")
	}
	creds, err := refreshStoredCredentials(ctx, client, rejected.AccessToken)
	if err != nil {
		return Credentials{}, err
	}
	creds.BaseURL = rejected.BaseURL
	return creds, nil
}

// refreshStoredCredentials refreshes the store under its lock when its access
// token is expiring or is the rejected one; a token another process renewed
// meanwhile is used as is.
func refreshStoredCredentials(ctx context.Context, client *http.Client, rejected string) (Credentials, error) {
	unlock, err := lockAuthStore(ctx)
	if err != nil {
		return Credentials{}, err
	}
	defer unlock()
	creds, ok, err := readMatrixclawCredentials()
	if err != nil {
		return Credentials{}, err
	}
	if !ok {
		return Credentials{}, reauthRequired("auth store disappeared during refresh")
	}
	if !tokenExpiring(creds.AccessToken, refreshSkew) && creds.AccessToken != rejected {
		return creds, nil
	}
	// The refresh token is single use: once the server rotates it, the new one
	// must be saved even when the run that asked is canceled meanwhile.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultRequestTimeout)
	defer cancel()
	access, refresh, err := tokenRequest(refreshCtx, client, "refresh", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {oauthClientID},
	})
	if err != nil {
		return Credentials{}, err
	}
	refreshed := Credentials{
		AccessToken:  access,
		RefreshToken: textutil.FirstNonEmpty(refresh, creds.RefreshToken),
		BaseURL:      creds.BaseURL,
		Source:       sourceStore,
	}
	if err := saveCredentials(refreshed); err != nil {
		return Credentials{}, err
	}
	return refreshed, nil
}

// tokenRequest posts form to the OAuth token endpoint; a rejected grant means
// the user must sign in again.
func tokenRequest(ctx context.Context, client *http.Client, what string, form url.Values) (access string, refresh string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("openai-codex: %s token: %w", what, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", "", fmt.Errorf("openai-codex: read %s response: %w", what, err)
	}
	if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnauthorized {
		return "", "", reauthRequired(what + " rejected: " + parseError(raw).Message)
	}
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("openai-codex: %s failed: %s", what, parseError(raw).Message)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", "", fmt.Errorf("openai-codex: decode %s response: %w", what, err)
	}
	access, refresh = strings.TrimSpace(payload.AccessToken), strings.TrimSpace(payload.RefreshToken)
	if access == "" {
		return "", "", fmt.Errorf("openai-codex: %s response missing access token", what)
	}
	return access, refresh, nil
}
