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
	"strings"
	"time"
)

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

func StartDeviceLogin(ctx context.Context, client *http.Client) (LoginDevice, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
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
		return LoginDevice{}, fmt.Errorf("openai-codex: device code request failed: %s", parseError(raw).Message)
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
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	if device.authID == "" || device.UserCode == "" {
		return LoginResult{}, errors.New("openai-codex: incomplete device login")
	}
	deadline := time.Now().Add(device.ExpiresIn)
	if !device.requestedAt.IsZero() {
		deadline = device.requestedAt.Add(device.ExpiresIn)
	}
	authCode, verifier, err := pollDeviceLogin(ctx, client, device, deadline)
	if err != nil {
		return LoginResult{}, err
	}
	access, refresh, err := tokenRequest(ctx, client, "token exchange", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"redirect_uri":  {oauthIssuer + "/deviceauth/callback"},
		"client_id":     {oauthClientID},
		"code_verifier": {verifier},
	})
	if err != nil {
		return LoginResult{}, err
	}
	if refresh == "" {
		return LoginResult{}, errors.New("openai-codex: token exchange response missing refresh token")
	}
	creds := Credentials{AccessToken: access, RefreshToken: refresh, BaseURL: DefaultBaseURL, Source: "device-code"}
	unlock, err := lockAuthStore(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	defer unlock()
	if err := saveCredentials(creds); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Credentials: creds}, nil
}

// pollDeviceLogin waits until the user approves the code in the browser.
func pollDeviceLogin(ctx context.Context, client *http.Client, device LoginDevice, deadline time.Time) (code string, verifier string, err error) {
	for {
		if time.Now().After(deadline) {
			return "", "", errors.New("openai-codex: device login timed out")
		}
		timer := time.NewTimer(device.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", "", ctx.Err()
		case <-timer.C:
		}
		body, _ := json.Marshal(map[string]string{"device_auth_id": device.authID, "user_code": device.UserCode})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthIssuer+"/api/accounts/deviceauth/token", bytes.NewReader(body))
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return "", "", fmt.Errorf("openai-codex: poll device login: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			return "", "", fmt.Errorf("openai-codex: read device poll: %w", readErr)
		}
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound {
			continue
		}
		if res.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("openai-codex: device login failed: %s", parseError(raw).Message)
		}
		var poll struct {
			AuthorizationCode string `json:"authorization_code"`
			CodeVerifier      string `json:"code_verifier"`
		}
		if err := json.Unmarshal(raw, &poll); err != nil {
			return "", "", fmt.Errorf("openai-codex: decode device poll: %w", err)
		}
		code, verifier = strings.TrimSpace(poll.AuthorizationCode), strings.TrimSpace(poll.CodeVerifier)
		if code == "" || verifier == "" {
			return "", "", errors.New("openai-codex: device login missing authorization code")
		}
		return code, verifier, nil
	}
}

// loginDeviceToken is how a pending device login travels through a client.
type loginDeviceToken struct {
	UserCode    string `json:"user_code"`
	VerifyURL   string `json:"verify_url"`
	IntervalMS  int64  `json:"interval_ms"`
	ExpiresMS   int64  `json:"expires_ms"`
	AuthID      string `json:"auth_id"`
	RequestedAt int64  `json:"requested_at"`
}

func EncodeLoginDevice(device LoginDevice) (string, error) {
	if strings.TrimSpace(device.authID) == "" || strings.TrimSpace(device.UserCode) == "" {
		return "", errors.New("openai-codex: incomplete device login")
	}
	raw, err := json.Marshal(loginDeviceToken{
		UserCode:    strings.TrimSpace(device.UserCode),
		VerifyURL:   strings.TrimSpace(device.VerifyURL),
		IntervalMS:  device.Interval.Milliseconds(),
		ExpiresMS:   device.ExpiresIn.Milliseconds(),
		AuthID:      strings.TrimSpace(device.authID),
		RequestedAt: device.requestedAt.UnixMilli(),
	})
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
	var payload loginDeviceToken
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
