package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	xaiBaseURL          = "https://api.x.ai/v1"
	defaultTTSVoiceID   = "eve"
	defaultLanguage     = "auto"
	defaultTTSName      = "matrixclaw-tts.mp3"
	defaultTTSMIME      = "audio/mpeg"
	maxTTSResponseBytes = 32 << 20
)

var (
	ErrModuleDisabled      = errors.New("voice module is disabled")
	ErrUnsupportedProvider = errors.New("voice provider is not supported yet")
)

type setupLoader interface {
	Load() (setup.Config, error)
}

type Service struct {
	setup     setupLoader
	client    *http.Client
	endpoint  string
	userAgent string
}

func NewService(setupService setupLoader) *Service {
	return &Service{
		setup:     setupService,
		client:    &http.Client{Timeout: 90 * time.Second},
		endpoint:  xaiBaseURL,
		userAgent: "matrixclaw",
	}
}

func (s *Service) TextToSpeech(ctx context.Context, req TextToSpeechRequest) (TextToSpeechResponse, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return TextToSpeechResponse{}, errors.New("text is required")
	}
	module, err := s.voiceModule(setup.VoiceModuleTTS)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	if !module.Enabled {
		return TextToSpeechResponse{}, ErrModuleDisabled
	}
	if module.ProviderID == "piper" {
		return s.piperTextToSpeech(ctx, module, text)
	}
	if module.ProviderID != "" && module.ProviderID != "grok" {
		return TextToSpeechResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, module.ProviderID)
	}
	if err := s.requireModule(ctx, setup.VoiceModuleTTS); err != nil {
		return TextToSpeechResponse{}, err
	}
	apiKey, err := s.xaiAPIKey()
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = defaultLanguage
	}
	voiceID := strings.TrimSpace(req.VoiceID)
	if voiceID == "" {
		voiceID = defaultTTSVoiceID
	}
	payload, err := json.Marshal(map[string]string{
		"text":     text,
		"voice_id": voiceID,
		"language": language,
	})
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.endpoint, "/")+"/tts", bytes.NewReader(payload))
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", s.userAgent)
	resp, err := s.httpClient().Do(httpReq)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxTTSResponseBytes+1))
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	if int64(len(content)) > maxTTSResponseBytes {
		return TextToSpeechResponse{}, errors.New("TTS response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TextToSpeechResponse{}, xaiError("tts", resp.StatusCode, content)
	}
	return NewTextToSpeechResponse(content, firstNonEmpty(resp.Header.Get("Content-Type"), defaultTTSMIME), defaultTTSName), nil
}

func (s *Service) piperTextToSpeech(ctx context.Context, module setup.VoiceModuleDescriptor, text string) (TextToSpeechResponse, error) {
	provider, ok := voiceProviderByID(module, "piper")
	if !ok {
		return TextToSpeechResponse{}, errors.New("Piper provider is not available")
	}
	content, err := localruntime.New("").PiperTextToSpeech(ctx, provider, text)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	return NewTextToSpeechResponse(content, "audio/wav", "matrixclaw-tts.wav"), nil
}

func (s *Service) SpeechToText(ctx context.Context, req SpeechToTextRequest) (SpeechToTextResponse, error) {
	content, err := req.ContentBytes()
	if err != nil {
		return SpeechToTextResponse{}, errors.New("content_base64 is invalid")
	}
	if len(content) == 0 {
		return SpeechToTextResponse{}, errors.New("audio content is required")
	}
	module, err := s.voiceModule(setup.VoiceModuleSTT)
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	if !module.Enabled {
		return SpeechToTextResponse{}, ErrModuleDisabled
	}
	if module.ProviderID == "whispercpp" {
		return s.whisperCppSpeechToText(ctx, module, req, content)
	}
	if module.ProviderID != "" && module.ProviderID != "grok" {
		return SpeechToTextResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, module.ProviderID)
	}
	apiKey, err := s.xaiAPIKey()
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "audio.ogg"
	}
	mimeType := strings.TrimSpace(req.MIMEType)
	if mimeType == "" {
		mimeType = mimeTypeFromName(fileName)
	}
	part, err := writer.CreateFormFile("file", filepath.Base(fileName))
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	if _, err := part.Write(content); err != nil {
		return SpeechToTextResponse{}, err
	}
	if language := strings.TrimSpace(req.Language); language != "" {
		_ = writer.WriteField("language", language)
	}
	_ = writer.Close()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.endpoint, "/")+"/stt", body)
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("User-Agent", s.userAgent)
	if mimeType != "" {
		httpReq.Header.Set("X-Matrixclaw-Audio-Mime", mimeType)
	}
	resp, err := s.httpClient().Do(httpReq)
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechToTextResponse{}, xaiError("stt", resp.StatusCode, responseBody)
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return SpeechToTextResponse{}, fmt.Errorf("decode stt response: %w", err)
	}
	return SpeechToTextResponse{Text: strings.TrimSpace(payload.Text)}, nil
}

func (s *Service) whisperCppSpeechToText(ctx context.Context, module setup.VoiceModuleDescriptor, req SpeechToTextRequest, content []byte) (SpeechToTextResponse, error) {
	provider, ok := voiceProviderByID(module, "whispercpp")
	if !ok {
		return SpeechToTextResponse{}, errors.New("Whisper.cpp provider is not available")
	}
	text, err := localruntime.New("").WhisperSpeechToText(ctx, provider, localruntime.WhisperSpeechInput{
		Content:  content,
		FileName: req.FileName,
		MIMEType: req.MIMEType,
		Language: firstNonEmpty(strings.TrimSpace(req.Language), strings.TrimSpace(provider.Config.Language)),
	})
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	return SpeechToTextResponse{Text: strings.TrimSpace(text)}, nil
}

func (s *Service) requireModule(_ context.Context, moduleID string) error {
	if s == nil || s.setup == nil {
		return errors.New("setup service is not configured")
	}
	cfg, err := s.setup.Load()
	if err != nil {
		return err
	}
	module := cfg.Modules.TextToSpeech
	if moduleID == setup.VoiceModuleSTT {
		module = cfg.Modules.SpeechToText
	}
	if !module.Enabled {
		return ErrModuleDisabled
	}
	if providerID := strings.TrimSpace(module.ProviderID); providerID != "" && providerID != "grok" {
		switch {
		case moduleID == setup.VoiceModuleTTS && providerID == "piper":
			return fmt.Errorf("local TTS provider Piper is selected but the local runtime manager is not available yet. Open Modules -> Text to Speech -> Provider -> Piper -> Status")
		case moduleID == setup.VoiceModuleSTT && providerID == "whispercpp":
			return nil
		default:
			return fmt.Errorf("%w: %s", ErrUnsupportedProvider, providerID)
		}
	}
	return nil
}

func (s *Service) voiceModule(moduleID string) (setup.VoiceModuleDescriptor, error) {
	if s == nil || s.setup == nil {
		return setup.VoiceModuleDescriptor{}, errors.New("setup service is not configured")
	}
	cfg, err := s.setup.Load()
	if err != nil {
		return setup.VoiceModuleDescriptor{}, err
	}
	modules := localruntime.New("").DecorateVoiceModules(setup.VoiceModuleDescriptors(cfg.Modules))
	for _, module := range modules {
		if module.ID == moduleID {
			return module, nil
		}
	}
	return setup.VoiceModuleDescriptor{}, nil
}

func voiceProviderByID(module setup.VoiceModuleDescriptor, providerID string) (setup.VoiceProviderOption, bool) {
	for _, provider := range module.Providers {
		if strings.EqualFold(provider.ID, providerID) {
			return provider, true
		}
	}
	return setup.VoiceProviderOption{}, false
}

func (s *Service) xaiAPIKey() (string, error) {
	cfg, err := s.setup.Load()
	if err != nil {
		return "", err
	}
	for _, provider := range cfg.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.ID), "xai") || strings.EqualFold(strings.TrimSpace(provider.CatalogID), "xai") {
			if key, ok := setup.ResolvedProviderAPIKey(provider); ok {
				return key, nil
			}
		}
	}
	if key := strings.TrimSpace(os.Getenv("XAI_API_KEY")); key != "" {
		return key, nil
	}
	return "", errors.New("xAI API key is required; configure xAI provider or set XAI_API_KEY")
}

func (s *Service) httpClient() *http.Client {
	if s != nil && s.client != nil {
		return s.client
	}
	return http.DefaultClient
}

func xaiError(area string, status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil {
		encoded, _ := json.Marshal(payload.Error)
		message = strings.TrimSpace(string(encoded))
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("xai %s: status %d: %s", area, status, message)
}

func mimeTypeFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".oga", ".ogg", ".opus":
		return "audio/ogg"
	case ".webm":
		return "audio/webm"
	default:
		return "application/octet-stream"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
