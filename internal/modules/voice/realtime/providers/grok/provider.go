package grok

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	defaultWSURL       = "wss://api.x.ai/v1/realtime"
	defaultModel       = "grok-voice-latest"
	defaultVoice       = "eve"
	defaultDialTimeout = 20 * time.Second
	maxWSMessageBytes  = 8 << 20
	verifyCacheTTL     = 6 * time.Hour
	verifyErrorTTL     = 30 * time.Second
	verifyHTTPTimeout  = 2 * time.Second
)

var voiceModels = []string{
	"grok-voice-latest",
	"grok-voice-think-fast-1.0",
	"grok-voice-fast-1.0",
}

var voiceNames = []string{
	"eve",
	"ara",
	"rex",
	"sal",
	"leo",
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
		case strings.TrimSpace(cfg.ModelID) == "":
			status = "Model required"
		case !modelInList(cfg.ModelID, voiceModels):
			status = "Selected model is not available"
		default:
			status = "Ready"
		}
	}
	configured := keyValid && strings.TrimSpace(cfg.ModelID) != "" && modelInList(cfg.ModelID, voiceModels)
	if configured {
		status = "Ready"
	}
	return realtime.ProviderDescriptor{
		ID:         realtime.ProviderGrok,
		Name:       "Grok Voice",
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
		Models:        append([]string(nil), voiceModels...),
		Voices:        append([]string(nil), voiceNames...),
		InputFormats:  []realtime.AudioFormat{realtime.DefaultInputAudioFormat()},
		OutputFormats: []realtime.AudioFormat{realtime.DefaultOutputAudioFormat()},
	}
}

func (p *Provider) Connect(ctx context.Context, req realtime.ProviderConnectRequest) (realtime.ProviderConnection, error) {
	cfg := p.currentConfig(ctx)
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("grok voice: api key is required")
	}
	modelID := firstNonEmpty(req.ModelID, cfg.ModelID, defaultModel)
	if !modelInList(modelID, voiceModels) {
		return nil, fmt.Errorf("grok voice: model %q is not available", modelID)
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
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + strings.TrimSpace(cfg.APIKey)}},
	})
	if err != nil {
		return nil, fmt.Errorf("grok voice: websocket dial: %w", err)
	}
	conn.SetReadLimit(maxWSMessageBytes)
	live := &connection{
		conn:    conn,
		outputs: make(chan realtime.ProviderOutput, 64),
	}
	instructions := combinedSystemInstruction(cfg.SystemInstruction, req.SystemInstruction, language)
	if err := live.writeJSON(setupCtx, sessionUpdateMessage(voiceID, language, instructions, req.Tools)); err != nil {
		_ = live.Close(err)
		return nil, fmt.Errorf("grok voice: send session update: %w", err)
	}
	if err := live.waitSessionUpdated(setupCtx); err != nil {
		_ = live.Close(err)
		return nil, err
	}
	safego.Go("grok.live.readLoop", func() { live.readLoop(ctx) })
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
	if p == nil || strings.TrimSpace(cfg.APIKey) == "" {
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
			message := p.verifyErr
			auth := p.verifyAuth
			p.verifyMu.Unlock()
			return verifyResult{Valid: message == "", AuthError: auth, Message: message}
		}
	}
	p.verifyMu.Unlock()

	probeCtx, cancel := context.WithTimeout(ctx, verifyHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "https://api.x.ai/v1/models", nil)
	if err != nil {
		return verifyResult{Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	res, err := http.DefaultClient.Do(req)
	message := ""
	auth := false
	if err != nil {
		message = err.Error()
	} else {
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			message = fmt.Sprintf("xAI models returned HTTP %d", res.StatusCode)
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

type connection struct {
	conn        *websocket.Conn
	writeMu     sync.Mutex
	outputs     chan realtime.ProviderOutput
	closeOnce   sync.Once
	outputsOnce sync.Once
}

func (c *connection) Send(ctx context.Context, input realtime.ProviderInput) error {
	switch input.Type {
	case realtime.ProviderInputAudioAppend:
		audio := strings.TrimSpace(input.AudioBase64)
		if audio == "" {
			return nil
		}
		return c.writeJSON(ctx, map[string]any{"type": "input_audio_buffer.append", "audio": audio})
	case realtime.ProviderInputAudioEnd:
		return nil
	case realtime.ProviderInputTextAppend:
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil
		}
		if err := c.writeJSON(ctx, map[string]any{
			"type": "conversation.item.create",
			"item": map[string]any{
				"type":    "message",
				"role":    "user",
				"content": []map[string]string{{"type": "input_text", "text": text}},
			},
		}); err != nil {
			return err
		}
		if input.EndOfTurn {
			return c.writeJSON(ctx, map[string]any{"type": "response.create"})
		}
		return nil
	case realtime.ProviderInputCancel:
		return c.writeJSON(ctx, map[string]any{"type": "response.cancel"})
	case realtime.ProviderInputToolResult:
		for _, response := range input.ToolResponses {
			body, err := json.Marshal(response.Response)
			if err != nil {
				body = []byte(`{"content":"ok"}`)
			}
			if err := c.writeJSON(ctx, map[string]any{
				"type": "conversation.item.create",
				"item": map[string]any{
					"type":    "function_call_output",
					"call_id": strings.TrimSpace(response.ID),
					"output":  string(body),
				},
			}); err != nil {
				return err
			}
		}
		if len(input.ToolResponses) > 0 {
			return c.writeJSON(ctx, map[string]any{"type": "response.create"})
		}
		return nil
	default:
		return nil
	}
}

func (c *connection) Receive(ctx context.Context) (realtime.ProviderOutput, error) {
	select {
	case output, ok := <-c.outputs:
		if !ok {
			return realtime.ProviderOutput{}, io.EOF
		}
		return output, nil
	case <-ctx.Done():
		return realtime.ProviderOutput{}, ctx.Err()
	}
}

func (c *connection) Close(reason error) error {
	var err error
	c.closeOnce.Do(func() {
		status := websocket.StatusNormalClosure
		message := ""
		if reason != nil {
			status = websocket.StatusInternalError
			message = truncateReason(reason.Error())
		}
		err = c.conn.Close(status, message)
	})
	return err
}

func (c *connection) waitSessionUpdated(ctx context.Context) error {
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("grok voice: wait session update: %w", err)
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		var msg serverMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("grok voice: decode setup response: %w", err)
		}
		switch strings.TrimSpace(msg.Type) {
		case "session.updated":
			return nil
		case "error":
			if msg.Error != nil {
				return fmt.Errorf("grok voice: %s", firstNonEmpty(msg.Error.Message, msg.Error.Code, "session update failed"))
			}
			return errors.New("grok voice: session update failed")
		default:
			continue
		}
	}
}

func (c *connection) readLoop(ctx context.Context) {
	defer c.closeOutputs()
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}
			c.emit(ctx, realtime.ProviderOutput{Type: realtime.ProviderOutputError, Error: err.Error()})
			return
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		for _, output := range decodeServerOutputs(data) {
			c.emit(ctx, output)
		}
	}
}

func (c *connection) closeOutputs() {
	c.outputsOnce.Do(func() { close(c.outputs) })
}

func (c *connection) emit(ctx context.Context, output realtime.ProviderOutput) {
	select {
	case c.outputs <- output:
	case <-ctx.Done():
	}
}

func (c *connection) writeJSON(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, body)
}

func sessionUpdateMessage(voiceID string, language string, instructions string, tools []realtime.ToolDeclaration) map[string]any {
	session := map[string]any{
		"voice":        firstNonEmpty(voiceID, defaultVoice),
		"instructions": strings.TrimSpace(instructions),
		"turn_detection": map[string]any{
			"type":                "server_vad",
			"threshold":           0.5,
			"prefix_padding_ms":   500,
			"silence_duration_ms": 750,
		},
		"audio": map[string]any{
			"input": map[string]any{
				"format": map[string]any{"type": "audio/pcm", "rate": realtime.DefaultInputAudioFormat().SampleRateHz},
				"transcription": map[string]any{
					"model": "grok-transcribe",
				},
			},
			"output": map[string]any{
				"format": map[string]any{"type": "audio/pcm", "rate": realtime.DefaultOutputAudioFormat().SampleRateHz},
			},
		},
	}
	if languageHint := normalizeLanguageCode(language); languageHint != "" && languageHint != "auto" {
		session["audio"].(map[string]any)["input"].(map[string]any)["transcription"].(map[string]any)["language_hint"] = languageHint
	}
	if declarations := functionDeclarations(tools); len(declarations) > 0 {
		session["tools"] = declarations
	}
	return map[string]any{"type": "session.update", "session": session}
}

func functionDeclarations(tools []realtime.ToolDeclaration) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		decl := map[string]any{
			"type":        "function",
			"name":        name,
			"description": strings.TrimSpace(tool.Description),
		}
		if len(tool.Parameters) > 0 {
			var parameters any
			if err := json.Unmarshal(tool.Parameters, &parameters); err == nil {
				decl["parameters"] = sanitizeFunctionSchema(parameters)
			}
		}
		out = append(out, decl)
		if len(out) >= 128 {
			break
		}
	}
	return out
}

func sanitizeFunctionSchema(value any) any {
	switch item := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, child := range item {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "properties":
				if props, ok := child.(map[string]any); ok {
					cleanProps := make(map[string]any, len(props))
					for propName, propSchema := range props {
						propName = strings.TrimSpace(propName)
						if propName != "" {
							cleanProps[propName] = sanitizeFunctionSchema(propSchema)
						}
					}
					out[key] = cleanProps
				}
			case "items":
				out[key] = sanitizeFunctionSchema(child)
			case "enum":
				if enum := sanitizeFunctionEnum(child); len(enum) > 0 {
					out[key] = enum
				}
			case "type", "format", "description", "nullable", "required", "minimum", "maximum", "minitems", "maxitems", "minlength", "maxlength":
				out[key] = sanitizeFunctionSchema(child)
			default:
				continue
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(item))
		for _, child := range item {
			out = append(out, sanitizeFunctionSchema(child))
		}
		return out
	default:
		return value
	}
}

func sanitizeFunctionEnum(value any) []any {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func combinedSystemInstruction(base string, session string, language string) string {
	parts := []string{}
	if base = strings.TrimSpace(base); base != "" {
		parts = append(parts, base)
	}
	if session = strings.TrimSpace(session); session != "" {
		parts = append(parts, session)
	}
	if guard := languageSystemInstruction(language); guard != "" {
		parts = append(parts, guard)
	}
	return strings.Join(parts, "\n\n")
}

func languageSystemInstruction(language string) string {
	code := normalizeLanguageCode(language)
	if code == "" {
		return ""
	}
	if code == "auto" {
		return "Realtime voice language policy:\n" +
			"- Detect the human's language from the first meaningful speech and keep speaking that language for the rest of the conversation unless the human explicitly asks to change language.\n" +
			"- If the greeting or recent conversation is in a non-English language, continue in that non-English language instead of switching to English.\n" +
			"- If speech recognition is ambiguous, stay with the previously established conversation language."
	}
	name := languageDisplayName(code)
	return "Realtime voice language policy:\n" +
		"- Speak only in " + name + " (" + code + ") unless the human explicitly asks to change language.\n" +
		"- Keep pronunciation and accent natural for " + name + ".\n" +
		"- If speech recognition is ambiguous, stay in " + name + " instead of switching to another language."
}

func realtimeURL(raw string, modelID string) (string, error) {
	raw = firstNonEmpty(raw, defaultWSURL)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("grok voice: invalid websocket url: %w", err)
	}
	query := parsed.Query()
	if query.Get("model") == "" {
		query.Set("model", firstNonEmpty(modelID, defaultModel))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func normalizeConfig(cfg Config) Config {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.APIKeyEnv = strings.TrimSpace(cfg.APIKeyEnv)
	cfg.WSURL = strings.TrimSpace(cfg.WSURL)
	if cfg.WSURL == "" {
		cfg.WSURL = defaultWSURL
	}
	cfg.ModelID = firstNonEmpty(cfg.ModelID, defaultModel)
	cfg.VoiceID = firstNonEmpty(cfg.VoiceID, defaultVoice)
	cfg.Language = normalizeLanguageCode(cfg.Language)
	cfg.SystemInstruction = strings.TrimSpace(cfg.SystemInstruction)
	return cfg
}

func normalizeLanguageCode(language string) string {
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "en", "en-us", "en-gb", "ar-eg", "ar-sa", "ar-ae", "bn", "bn-bd", "zh", "zh-cn", "fr", "fr-fr", "de", "de-de", "hi", "hi-in", "id", "id-id", "it", "it-it", "ja", "ja-jp", "ko", "ko-kr", "ru", "ru-ru", "tr", "tr-tr", "vi", "vi-vn":
		return canonicalLanguage(value)
	case "pt", "pt-br":
		return "pt-BR"
	case "pt-pt":
		return "pt-PT"
	case "es", "es-mx":
		return "es-MX"
	case "es-es":
		return "es-ES"
	default:
		return strings.TrimSpace(language)
	}
}

func canonicalLanguage(value string) string {
	switch value {
	case "en-us", "en-gb":
		return "en"
	case "ar-eg":
		return "ar-EG"
	case "ar-sa":
		return "ar-SA"
	case "ar-ae":
		return "ar-AE"
	case "bn-bd":
		return "bn"
	case "zh-cn":
		return "zh"
	case "fr-fr":
		return "fr"
	case "de-de":
		return "de"
	case "hi-in":
		return "hi"
	case "id-id":
		return "id"
	case "it-it":
		return "it"
	case "ja-jp":
		return "ja"
	case "ko-kr":
		return "ko"
	case "ru-ru":
		return "ru"
	case "tr-tr":
		return "tr"
	case "vi-vn":
		return "vi"
	default:
		return value
	}
}

func languageDisplayName(language string) string {
	switch normalizeLanguageCode(language) {
	case "en":
		return "English"
	case "ar-EG":
		return "Arabic (Egypt)"
	case "ar-SA":
		return "Arabic (Saudi Arabia)"
	case "ar-AE":
		return "Arabic (United Arab Emirates)"
	case "bn":
		return "Bengali"
	case "zh":
		return "Chinese"
	case "fr":
		return "French"
	case "de":
		return "German"
	case "hi":
		return "Hindi"
	case "id":
		return "Indonesian"
	case "it":
		return "Italian"
	case "ja":
		return "Japanese"
	case "ko":
		return "Korean"
	case "pt-BR":
		return "Portuguese (Brazil)"
	case "pt-PT":
		return "Portuguese (Portugal)"
	case "ru":
		return "Russian"
	case "es-MX":
		return "Spanish (Mexico)"
	case "es-ES":
		return "Spanish (Spain)"
	case "tr":
		return "Turkish"
	case "vi":
		return "Vietnamese"
	default:
		if code := strings.TrimSpace(language); code != "" {
			return code
		}
		return "the configured language"
	}
}

func modelInList(modelID string, models []string) bool {
	modelID = strings.TrimSpace(modelID)
	for _, candidate := range models {
		if strings.EqualFold(modelID, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
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
