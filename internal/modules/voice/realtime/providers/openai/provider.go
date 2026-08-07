package openai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/coder/websocket"
)

const (
	defaultWSURL       = "wss://api.openai.com/v1/realtime"
	defaultModel       = "gpt-realtime-2.1"
	defaultVoice       = "marin"
	defaultDialTimeout = 20 * time.Second
	maxWSMessageBytes  = 8 << 20
	verifyCacheTTL     = 6 * time.Hour
	verifyErrorTTL     = 30 * time.Second
	verifyHTTPTimeout  = 2 * time.Second
)

var realtimeModels = []string{
	"gpt-realtime-2.1",
	"gpt-realtime-2.1-mini",
}

var voiceNames = []string{
	"alloy",
	"ash",
	"ballad",
	"coral",
	"echo",
	"sage",
	"shimmer",
	"verse",
	"marin",
	"cedar",
}

type Config struct {
	APIKey            string
	APIKeyEnv         string
	WSURL             string
	ModelID           string
	VoiceID           string
	Language          string
	SystemInstruction string
	DialTimeout       time.Duration
}

type ConfigSource func(context.Context) Config

type Provider struct {
	config     Config
	source     ConfigSource
	verifyMu   sync.Mutex
	verifyAt   time.Time
	verifyKey  string
	verifyErr  string
	verifyAuth bool
}

func New(cfg Config) *Provider {
	return &Provider{config: normalizeConfig(cfg)}
}

func (p *Provider) SetConfigSource(source ConfigSource) *Provider {
	if p != nil {
		p.source = source
	}
	return p
}

func (p *Provider) Descriptor(ctx context.Context) realtime.ProviderDescriptor {
	cfg := p.currentConfig(ctx)
	keyConfigured := strings.TrimSpace(cfg.APIKey) != ""
	keyValid := false
	keyError := ""
	status := "API key required"
	if keyConfigured {
		result := p.verifyAPIKey(ctx, cfg)
		keyValid = result.Valid
		keyError = result.Message
		switch {
		case result.AuthError:
			status = "Invalid API key"
		case result.Message != "":
			status = "Could not verify API key"
		case !modelInList(cfg.ModelID, realtimeModels):
			status = "Selected model is not available"
		default:
			status = "Ready"
		}
	}
	configured := keyValid && modelInList(cfg.ModelID, realtimeModels)
	return realtime.ProviderDescriptor{
		ID:         realtime.ProviderOpenAI,
		Name:       "OpenAI Realtime",
		Status:     status,
		Configured: configured,
		Config: realtime.ProviderConfigSummary{
			APIKeyConfigured: keyConfigured,
			APIKeyValid:      keyValid,
			APIKeyPreview:    maskSecret(cfg.APIKey),
			APIKeyError:      keyError,
			APIKeyEnv:        cfg.APIKeyEnv,
			ModelID:          cfg.ModelID,
			VoiceID:          cfg.VoiceID,
			Language:         cfg.Language,
			Endpoint:         cfg.WSURL,
		},
		DefaultModel:  defaultModel,
		Models:        append([]string(nil), realtimeModels...),
		Voices:        append([]string(nil), voiceNames...),
		InputFormats:  []realtime.AudioFormat{realtime.DefaultInputAudioFormat()},
		OutputFormats: []realtime.AudioFormat{realtime.DefaultOutputAudioFormat()},
	}
}

func (p *Provider) Connect(ctx context.Context, req realtime.ProviderConnectRequest) (realtime.ProviderConnection, error) {
	cfg := p.currentConfig(ctx)
	if cfg.APIKey == "" {
		return nil, errors.New("openai realtime: api key is required")
	}
	modelID := firstNonEmpty(req.ModelID, cfg.ModelID, defaultModel)
	if !modelInList(modelID, realtimeModels) {
		return nil, fmt.Errorf("openai realtime: model %q is not available", modelID)
	}
	voiceID := firstNonEmpty(req.VoiceID, cfg.VoiceID, defaultVoice)
	language := firstNonEmpty(req.Language, cfg.Language)
	endpoint, err := realtimeURL(cfg.WSURL, modelID)
	if err != nil {
		return nil, err
	}

	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	setupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, _, err := websocket.Dial(setupCtx, endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + cfg.APIKey}},
	})
	if err != nil {
		return nil, fmt.Errorf("openai realtime: websocket dial: %w", err)
	}
	conn.SetReadLimit(maxWSMessageBytes)
	live := &connection{
		conn:      conn,
		decoder:   newMessageDecoder(),
		resampler: newPCM16Resampler(req.InputAudio.SampleRateHz, 24000),
		outputs:   make(chan realtime.ProviderOutput, 64),
	}
	instructions := combinedSystemInstruction(cfg.SystemInstruction, req.SystemInstruction, language)
	if err := live.writeJSON(setupCtx, sessionUpdateMessage(modelID, voiceID, language, instructions, req.Tools)); err != nil {
		_ = live.Close(err)
		return nil, fmt.Errorf("openai realtime: send session update: %w", err)
	}
	if err := live.waitSessionUpdated(setupCtx); err != nil {
		_ = live.Close(err)
		return nil, err
	}
	safego.Go("openai.realtime.readLoop", func() { live.readLoop(ctx) })
	return live, nil
}

func (p *Provider) currentConfig(ctx context.Context) Config {
	if p == nil {
		return normalizeConfig(Config{})
	}
	if p.source != nil {
		return normalizeConfig(p.source(ctx))
	}
	return normalizeConfig(p.config)
}

type verifyResult struct {
	Valid     bool
	AuthError bool
	Message   string
}

func (p *Provider) verifyAPIKey(ctx context.Context, cfg Config) verifyResult {
	if p == nil || cfg.APIKey == "" {
		return verifyResult{}
	}
	cacheKey := secretCacheKey(cfg.APIKey)
	now := time.Now()
	p.verifyMu.Lock()
	if p.verifyKey == cacheKey && !p.verifyAt.IsZero() {
		ttl := verifyCacheTTL
		if p.verifyErr != "" {
			ttl = verifyErrorTTL
		}
		if now.Sub(p.verifyAt) < ttl {
			result := verifyResult{
				Valid:     p.verifyErr == "",
				AuthError: p.verifyAuth,
				Message:   p.verifyErr,
			}
			p.verifyMu.Unlock()
			return result
		}
	}
	p.verifyMu.Unlock()

	probeCtx, cancel := context.WithTimeout(ctx, verifyHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return verifyResult{Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	res, err := http.DefaultClient.Do(req)
	message := ""
	auth := false
	if err != nil {
		message = err.Error()
	} else {
		defer func() { _ = res.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			message = fmt.Sprintf("OpenAI models returned HTTP %d", res.StatusCode)
			auth = res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden
		}
	}
	p.verifyMu.Lock()
	p.verifyKey = cacheKey
	p.verifyAt = now
	p.verifyErr = message
	p.verifyAuth = auth
	p.verifyMu.Unlock()
	return verifyResult{Valid: message == "", AuthError: auth, Message: message}
}

func realtimeURL(raw string, modelID string) (string, error) {
	parsed, err := url.Parse(firstNonEmpty(raw, defaultWSURL))
	if err != nil {
		return "", fmt.Errorf("openai realtime: invalid websocket url: %w", err)
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", errors.New("openai realtime: websocket url must use ws or wss")
	}
	query := parsed.Query()
	query.Set("model", firstNonEmpty(modelID, defaultModel))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func normalizeConfig(cfg Config) Config {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.APIKeyEnv = strings.TrimSpace(cfg.APIKeyEnv)
	cfg.WSURL = firstNonEmpty(cfg.WSURL, defaultWSURL)
	cfg.ModelID = firstNonEmpty(cfg.ModelID, defaultModel)
	cfg.VoiceID = firstNonEmpty(cfg.VoiceID, defaultVoice)
	cfg.Language = normalizeLanguageCode(cfg.Language)
	cfg.SystemInstruction = strings.TrimSpace(cfg.SystemInstruction)
	return cfg
}

func modelInList(modelID string, models []string) bool {
	for _, candidate := range models {
		if strings.EqualFold(strings.TrimSpace(modelID), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func secretCacheKey(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func maskSecret(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

func truncateReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 120 {
		return value
	}
	return value[:120]
}
