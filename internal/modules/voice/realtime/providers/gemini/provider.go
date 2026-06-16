package gemini

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
	"nhooyr.io/websocket"
)

const (
	defaultWSURL       = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
	defaultVoice       = "Puck"
	defaultDialTimeout = 20 * time.Second
	maxWSMessageBytes  = 8 << 20
	modelsCacheTTL     = 6 * time.Hour
	modelsErrorTTL     = 30 * time.Second
	modelsHTTPTimeout  = 2 * time.Second
)

var knownDeveloperLiveModels = []string{
	"gemini-3.1-flash-live-preview",
	"gemini-2.5-flash-native-audio-preview-12-2025",
	"gemini-2.5-flash-native-audio-preview-09-2025",
}

var liveVoiceNames = []string{
	"Zephyr",
	"Puck",
	"Charon",
	"Kore",
	"Fenrir",
	"Leda",
	"Orus",
	"Aoede",
	"Callirrhoe",
	"Autonoe",
	"Enceladus",
	"Iapetus",
	"Umbriel",
	"Algieba",
	"Despina",
	"Erinome",
	"Algenib",
	"Rasalgethi",
	"Laomedeia",
	"Achernar",
	"Alnilam",
	"Schedar",
	"Gacrux",
	"Pulcherrima",
	"Achird",
	"Zubenelgenubi",
	"Vindemiatrix",
	"Sadachbia",
	"Sadaltager",
	"Sulafat",
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
	modelsMu   sync.Mutex
	models     []string
	modelsAt   time.Time
	modelsKey  string
	modelsErr  string
	modelsAuth bool
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
	status := "API key required"
	models := []string{}
	keyValid := false
	modelError := ""
	if keyConfigured {
		result := p.liveModels(ctx, cfg)
		models = result.Models
		keyValid = result.Valid
		modelError = result.Message
		switch {
		case result.AuthError:
			status = "Invalid API key"
		case strings.TrimSpace(result.Message) != "":
			status = "Could not verify API key"
		case len(result.Models) == 0:
			status = "No realtime models available"
		case strings.TrimSpace(cfg.ModelID) == "":
			status = "Model required"
		case !modelInList(cfg.ModelID, result.Models):
			status = "Selected model is not available"
		default:
			status = "Ready"
		}
	}
	configured := keyValid && strings.TrimSpace(cfg.ModelID) != "" && modelInList(cfg.ModelID, models)
	if configured {
		status = "Ready"
	}
	return realtime.ProviderDescriptor{
		ID:         realtime.ProviderGemini,
		Name:       "Gemini Live",
		Status:     status,
		Configured: configured,
		Config: realtime.ProviderConfigSummary{
			APIKeyConfigured: keyConfigured,
			APIKeyValid:      keyValid,
			APIKeyPreview:    maskSecret(cfg.APIKey),
			APIKeyError:      modelError,
			APIKeyEnv:        cfg.APIKeyEnv,
			ModelID:          cfg.ModelID,
			VoiceID:          cfg.VoiceID,
			Language:         cfg.Language,
			Endpoint:         cfg.WSURL,
		},
		Models:        models,
		Voices:        append([]string(nil), liveVoiceNames...),
		InputFormats:  []realtime.AudioFormat{realtime.DefaultInputAudioFormat()},
		OutputFormats: []realtime.AudioFormat{realtime.DefaultOutputAudioFormat()},
	}
}

func (p *Provider) Connect(ctx context.Context, req realtime.ProviderConnectRequest) (realtime.ProviderConnection, error) {
	cfg := p.currentConfig(ctx)
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("gemini live: api key is required")
	}
	modelID := firstNonEmpty(req.ModelID, cfg.ModelID)
	if modelID == "" {
		return nil, errors.New("gemini live: model is required")
	}
	voiceID := firstNonEmpty(req.VoiceID, cfg.VoiceID, defaultVoice)
	language := firstNonEmpty(req.Language, cfg.Language)
	endpoint, err := liveURL(cfg.WSURL, cfg.APIKey)
	if err != nil {
		return nil, err
	}

	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	setupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, _, err := websocket.Dial(setupCtx, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini live: websocket dial: %w", err)
	}
	conn.SetReadLimit(maxWSMessageBytes)
	live := &connection{
		conn:         conn,
		realtimeText: usesRealtimeText(modelID),
		outputs:      make(chan realtime.ProviderOutput, 64),
	}
	systemInstruction := combinedSystemInstruction(cfg.SystemInstruction, req.SystemInstruction, language)
	if err := live.writeJSON(setupCtx, setupMessage(modelID, voiceID, language, systemInstruction, req.Tools)); err != nil {
		_ = live.Close(err)
		return nil, fmt.Errorf("gemini live: send setup: %w", err)
	}
	if err := live.waitSetupComplete(setupCtx); err != nil {
		_ = live.Close(err)
		return nil, err
	}
	go live.readLoop(ctx)
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

type liveModelsResult struct {
	Models    []string
	Valid     bool
	AuthError bool
	Message   string
}

func (p *Provider) liveModels(ctx context.Context, cfg Config) liveModelsResult {
	if p == nil || strings.TrimSpace(cfg.APIKey) == "" {
		return liveModelsResult{}
	}
	cacheKey := secretCacheKey(cfg.APIKey)
	now := time.Now()
	p.modelsMu.Lock()
	if p.modelsKey == cacheKey && !p.modelsAt.IsZero() {
		ttl := modelsCacheTTL
		if p.modelsErr != "" {
			ttl = modelsErrorTTL
		}
		if now.Sub(p.modelsAt) < ttl {
			models := append([]string(nil), p.models...)
			message := p.modelsErr
			authError := p.modelsAuth
			p.modelsMu.Unlock()
			return liveModelsResult{
				Models:    models,
				Valid:     message == "",
				AuthError: authError,
				Message:   message,
			}
		}
	}
	p.modelsMu.Unlock()

	models, err := fetchLiveModels(ctx, cfg)
	result := liveModelsResult{Models: models, Valid: err == nil}
	if err != nil {
		result.AuthError = isAuthError(err)
		result.Message = err.Error()
	}
	p.modelsMu.Lock()
	p.models = append([]string(nil), models...)
	p.modelsAt = now
	p.modelsKey = cacheKey
	p.modelsErr = result.Message
	p.modelsAuth = result.AuthError
	p.modelsMu.Unlock()
	return result
}

type connection struct {
	conn         *websocket.Conn
	writeMu      sync.Mutex
	realtimeText bool
	outputs      chan realtime.ProviderOutput
	closeOnce    sync.Once
	outputsOnce  sync.Once
}

func (c *connection) Send(ctx context.Context, input realtime.ProviderInput) error {
	switch input.Type {
	case realtime.ProviderInputAudioAppend:
		return c.writeJSON(ctx, inputAudioAppendMessage(input.AudioBase64, input.AudioMIMEType))
	case realtime.ProviderInputAudioEnd:
		return c.writeJSON(ctx, inputAudioStreamEndMessage())
	case realtime.ProviderInputTextAppend:
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil
		}
		if input.EndOfTurn {
			return c.writeJSON(ctx, map[string]any{
				"clientContent": map[string]any{
					"turns": []map[string]any{{
						"role":  "user",
						"parts": []map[string]string{{"text": text}},
					}},
					"turnComplete": true,
				},
			})
		}
		return c.writeJSON(ctx, map[string]any{"realtimeInput": map[string]any{"text": text}})
	case realtime.ProviderInputCancel:
		return nil
	case realtime.ProviderInputToolResult:
		if len(input.ToolResponses) == 0 {
			return nil
		}
		responses := make([]map[string]any, 0, len(input.ToolResponses))
		for _, response := range input.ToolResponses {
			responses = append(responses, map[string]any{
				"id":       strings.TrimSpace(response.ID),
				"name":     strings.TrimSpace(response.Name),
				"response": response.Response,
			})
		}
		return c.writeJSON(ctx, map[string]any{"toolResponse": map[string]any{"functionResponses": responses}})
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
		c.closeOutputs()
	})
	return err
}

func (c *connection) waitSetupComplete(ctx context.Context) error {
	messageType, data, err := c.conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("gemini live: wait setup complete: %w", err)
	}
	if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
		return errors.New("gemini live: setup response was not JSON")
	}
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("gemini live: decode setup response: %w", err)
	}
	if msg.SetupComplete == nil {
		return fmt.Errorf("gemini live: expected setupComplete, got %s", strings.TrimSpace(string(data)))
	}
	return nil
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
		outputs := decodeServerOutputs(data)
		for _, output := range outputs {
			c.emit(ctx, output)
		}
	}
}

func (c *connection) closeOutputs() {
	c.outputsOnce.Do(func() {
		close(c.outputs)
	})
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

func inputAudioAppendMessage(audioBase64 string, mimeType string) map[string]any {
	return map[string]any{
		"realtimeInput": map[string]any{
			"audio": map[string]any{
				"data":     strings.TrimSpace(audioBase64),
				"mimeType": firstNonEmpty(mimeType, "audio/pcm;rate=16000"),
			},
		},
	}
}

func inputAudioStreamEndMessage() map[string]any {
	return map[string]any{"realtimeInput": map[string]any{"audioStreamEnd": true}}
}

func setupMessage(modelID string, voiceID string, language string, systemInstruction string, tools []realtime.ToolDeclaration) map[string]any {
	setup := map[string]any{
		"model": "models/" + strings.TrimPrefix(strings.TrimSpace(modelID), "models/"),
		"generationConfig": map[string]any{
			"responseModalities": []string{"AUDIO"},
			"thinkingConfig":     thinkingConfig(modelID),
		},
		"inputAudioTranscription":  map[string]any{},
		"outputAudioTranscription": map[string]any{},
		"realtimeInputConfig": map[string]any{
			"activityHandling": "START_OF_ACTIVITY_INTERRUPTS",
			"turnCoverage":     "TURN_INCLUDES_ONLY_ACTIVITY",
			"automaticActivityDetection": map[string]any{
				"disabled":                 false,
				"startOfSpeechSensitivity": "START_SENSITIVITY_HIGH",
				"prefixPaddingMs":          200,
				"silenceDurationMs":        600,
				"endOfSpeechSensitivity":   "END_SENSITIVITY_HIGH",
			},
		},
	}
	speechConfig := map[string]any{}
	if voiceID != "" {
		speechConfig["voiceConfig"] = map[string]any{
			"prebuiltVoiceConfig": map[string]any{"voiceName": voiceID},
		}
	}
	if languageCode := speechConfigLanguageCode(modelID, language); languageCode != "" {
		speechConfig["languageCode"] = languageCode
	}
	if len(speechConfig) > 0 {
		setup["generationConfig"].(map[string]any)["speechConfig"] = speechConfig
	}
	if strings.TrimSpace(systemInstruction) != "" {
		setup["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": strings.TrimSpace(systemInstruction)}},
		}
	}
	if declarations := functionDeclarations(tools); len(declarations) > 0 {
		setup["tools"] = []map[string]any{{"functionDeclarations": declarations}}
	}
	return map[string]any{"setup": setup}
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
	if strings.EqualFold(code, "auto") {
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

func speechConfigLanguageCode(modelID string, language string) string {
	code := normalizeLanguageCode(language)
	if code == "" || strings.EqualFold(code, "auto") {
		return ""
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(modelID)), "native-audio") {
		return ""
	}
	return code
}

func normalizeLanguageCode(language string) string {
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "ar", "ar-eg":
		return "ar-EG"
	case "bn", "bn-bd":
		return "bn-BD"
	case "nl", "nl-nl":
		return "nl-NL"
	case "en", "en-us":
		return "en-US"
	case "en-in":
		return "en-IN"
	case "fr", "fr-fr":
		return "fr-FR"
	case "de", "de-de":
		return "de-DE"
	case "hi", "hi-in":
		return "hi-IN"
	case "id", "id-id":
		return "id-ID"
	case "it", "it-it":
		return "it-IT"
	case "ja", "ja-jp":
		return "ja-JP"
	case "ko", "ko-kr":
		return "ko-KR"
	case "mr", "mr-in":
		return "mr-IN"
	case "pl", "pl-pl":
		return "pl-PL"
	case "pt", "pt-br":
		return "pt-BR"
	case "ro", "ro-ro":
		return "ro-RO"
	case "ru", "ru-ru":
		return "ru-RU"
	case "es", "es-us":
		return "es-US"
	case "ta", "ta-in":
		return "ta-IN"
	case "te", "te-in":
		return "te-IN"
	case "th", "th-th":
		return "th-TH"
	case "tr", "tr-tr":
		return "tr-TR"
	case "uk", "uk-ua":
		return "uk-UA"
	case "vi", "vi-vn":
		return "vi-VN"
	}
	parts := strings.Split(value, "-")
	if len(parts) >= 2 && len(parts[0]) == 2 && len(parts[1]) == 2 {
		return parts[0] + "-" + strings.ToUpper(parts[1])
	}
	return strings.TrimSpace(language)
}

func languageDisplayName(language string) string {
	switch normalizeLanguageCode(language) {
	case "ar-EG":
		return "Arabic (Egyptian)"
	case "bn-BD":
		return "Bengali (Bangladesh)"
	case "nl-NL":
		return "Dutch (Netherlands)"
	case "en-IN":
		return "English (India)"
	case "en-US":
		return "English (US)"
	case "fr-FR":
		return "French (France)"
	case "de-DE":
		return "German (Germany)"
	case "hi-IN":
		return "Hindi (India)"
	case "id-ID":
		return "Indonesian (Indonesia)"
	case "it-IT":
		return "Italian (Italy)"
	case "ja-JP":
		return "Japanese (Japan)"
	case "ko-KR":
		return "Korean (Korea)"
	case "mr-IN":
		return "Marathi (India)"
	case "pl-PL":
		return "Polish (Poland)"
	case "pt-BR":
		return "Portuguese (Brazil)"
	case "ro-RO":
		return "Romanian (Romania)"
	case "ru-RU":
		return "Russian (Russia)"
	case "es-US":
		return "Spanish (US)"
	case "ta-IN":
		return "Tamil (India)"
	case "te-IN":
		return "Telugu (India)"
	case "th-TH":
		return "Thai (Thailand)"
	case "tr-TR":
		return "Turkish (Turkey)"
	case "uk-UA":
		return "Ukrainian (Ukraine)"
	case "vi-VN":
		return "Vietnamese (Vietnam)"
	default:
		if code := strings.TrimSpace(language); code != "" {
			return code
		}
		return "the configured language"
	}
}

func thinkingConfig(modelID string) map[string]any {
	if usesRealtimeText(modelID) {
		return map[string]any{"thinkingLevel": "low"}
	}
	return map[string]any{"thinkingBudget": 0}
}

func usesRealtimeText(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return strings.Contains(modelID, "gemini-3") || strings.Contains(modelID, "3.1")
}

func functionDeclarations(tools []realtime.ToolDeclaration) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		decl := map[string]any{
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

func liveURL(raw string, apiKey string) (string, error) {
	raw = firstNonEmpty(raw, defaultWSURL)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("gemini live: invalid websocket url: %w", err)
	}
	query := parsed.Query()
	if query.Get("key") == "" {
		query.Set("key", strings.TrimSpace(apiKey))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

type liveModelsResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
	} `json:"models"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

type modelListError struct {
	StatusCode int
	Message    string
}

func (e modelListError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return fmt.Sprintf("models request returned HTTP %d", e.StatusCode)
}

func fetchLiveModels(ctx context.Context, cfg Config) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsHTTPTimeout)
	defer cancel()
	client := &http.Client{Timeout: modelsHTTPTimeout}
	out := []string{}
	pageToken := ""
	for {
		endpoint := "https://generativelanguage.googleapis.com/v1beta/models"
		if pageToken != "" {
			endpoint += "?pageToken=" + url.QueryEscape(pageToken)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-goog-api-key", strings.TrimSpace(cfg.APIKey))
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, modelListError{StatusCode: res.StatusCode, Message: decodeModelListError(res.StatusCode, body)}
		}
		var payload liveModelsResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Models {
			modelID := normalizeModelName(item.Name)
			if modelID == "" {
				continue
			}
			if liveVoiceModel(modelID, item.SupportedGenerationMethods) {
				out = append(out, modelID)
			}
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
		if pageToken == "" {
			break
		}
	}
	return mergeModels(nil, out), nil
}

func liveVoiceModel(modelID string, methods []string) bool {
	modelID = normalizeModelName(modelID)
	if strings.Contains(strings.ToLower(modelID), "live-translate") {
		return false
	}
	return liveModelSupportedByMethod(methods) || knownDeveloperLiveModel(modelID)
}

func liveModelSupportedByMethod(methods []string) bool {
	for _, method := range methods {
		method = strings.ToLower(strings.TrimSpace(method))
		if method == "bidigeneratecontent" || method == "bidi_generate_content" {
			return true
		}
	}
	return false
}

func knownDeveloperLiveModel(modelID string) bool {
	modelID = normalizeModelName(modelID)
	for _, candidate := range knownDeveloperLiveModels {
		if strings.EqualFold(modelID, candidate) {
			return true
		}
	}
	return false
}

func mergeModels(groups ...[]string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, modelID := range group {
			modelID = normalizeModelName(modelID)
			if modelID == "" {
				continue
			}
			key := strings.ToLower(modelID)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, modelID)
		}
	}
	return out
}

func normalizeModelName(modelID string) string {
	return strings.TrimPrefix(strings.TrimSpace(modelID), "models/")
}

func modelInList(modelID string, models []string) bool {
	modelID = normalizeModelName(modelID)
	if modelID == "" {
		return false
	}
	for _, candidate := range models {
		if strings.EqualFold(modelID, normalizeModelName(candidate)) {
			return true
		}
	}
	return false
}

func isAuthError(err error) bool {
	var modelErr modelListError
	if errors.As(err, &modelErr) {
		return modelErr.StatusCode == http.StatusUnauthorized || modelErr.StatusCode == http.StatusForbidden
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "api key not valid") ||
		strings.Contains(message, "invalid api key") ||
		strings.Contains(message, "permission denied") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden")
}

func decodeModelListError(statusCode int, body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		message := strings.TrimSpace(payload.Error.Message)
		if message != "" {
			return message
		}
		status := strings.TrimSpace(payload.Error.Status)
		if status != "" {
			return status
		}
	}
	return fmt.Sprintf("models request returned HTTP %d", statusCode)
}

func secretCacheKey(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func normalizeConfig(cfg Config) Config {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.APIKeyEnv = strings.TrimSpace(cfg.APIKeyEnv)
	cfg.WSURL = strings.TrimSpace(cfg.WSURL)
	if cfg.WSURL == "" {
		cfg.WSURL = defaultWSURL
	}
	cfg.ModelID = strings.TrimSpace(cfg.ModelID)
	cfg.VoiceID = strings.TrimSpace(cfg.VoiceID)
	if cfg.VoiceID == "" {
		cfg.VoiceID = defaultVoice
	}
	cfg.Language = normalizeLanguageCode(cfg.Language)
	cfg.SystemInstruction = strings.TrimSpace(cfg.SystemInstruction)
	return cfg
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
