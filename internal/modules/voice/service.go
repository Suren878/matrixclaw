package voice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	defaultTTSName = "matrixclaw-tts.mp3"
	defaultTTSMIME = "audio/mpeg"
)

var (
	ErrModuleDisabled      = errors.New("voice module is disabled")
	ErrUnsupportedProvider = errors.New("voice provider is not supported yet")
	ErrProviderUnavailable = errors.New("voice provider is unavailable")
	ErrInvalidRequest      = errors.New("invalid voice request")
)

type setupLoader interface {
	Load() (setup.Config, error)
}

type Service struct {
	setup setupLoader
}

func NewService(setupService setupLoader) *Service {
	return &Service{setup: setupService}
}

func (s *Service) TextToSpeech(ctx context.Context, req TextToSpeechRequest) (TextToSpeechResponse, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return TextToSpeechResponse{}, fmt.Errorf("%w: text is required", ErrInvalidRequest)
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
	if module.ProviderID == "supertonic" {
		return s.supertonicTextToSpeech(ctx, module, text)
	}
	if module.ProviderID != "" {
		return TextToSpeechResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, module.ProviderID)
	}
	return TextToSpeechResponse{}, ErrUnsupportedProvider
}

func (s *Service) piperTextToSpeech(ctx context.Context, module setup.VoiceModuleDescriptor, text string) (TextToSpeechResponse, error) {
	provider, ok := voiceProviderByID(module, "piper")
	if !ok {
		return TextToSpeechResponse{}, fmt.Errorf("%w: piper", ErrProviderUnavailable)
	}
	if err := ensureVoiceProviderAvailable(provider); err != nil {
		return TextToSpeechResponse{}, err
	}
	content, err := localruntime.New("").PiperTextToSpeech(ctx, provider, text)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	mp3, err := wavToMP3(ctx, content)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	return NewTextToSpeechResponse(mp3, defaultTTSMIME, defaultTTSName), nil
}

func (s *Service) supertonicTextToSpeech(ctx context.Context, module setup.VoiceModuleDescriptor, text string) (TextToSpeechResponse, error) {
	provider, ok := voiceProviderByID(module, "supertonic")
	if !ok {
		return TextToSpeechResponse{}, fmt.Errorf("%w: supertonic", ErrProviderUnavailable)
	}
	if err := ensureVoiceProviderAvailable(provider); err != nil {
		return TextToSpeechResponse{}, err
	}
	content, err := localruntime.New("").SupertonicTextToSpeech(ctx, provider, text)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	mp3, err := wavToMP3(ctx, content)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	return NewTextToSpeechResponse(mp3, defaultTTSMIME, defaultTTSName), nil
}

func wavToMP3(ctx context.Context, content []byte) ([]byte, error) {
	if len(content) == 0 {
		return nil, errors.New("audio content is empty")
	}
	cmd := exec.CommandContext(ctx,
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "wav",
		"-i", "pipe:0",
		"-codec:a", "libmp3lame",
		"-b:a", "96k",
		"-f", "mp3",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(content)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("ffmpeg is required to convert local TTS audio to MP3")
		}
		return nil, fmt.Errorf("convert local TTS audio to MP3: %s", message)
	}
	if stdout.Len() == 0 {
		return nil, errors.New("ffmpeg returned empty MP3 audio")
	}
	return stdout.Bytes(), nil
}

func (s *Service) SpeechToText(ctx context.Context, req SpeechToTextRequest) (SpeechToTextResponse, error) {
	content, err := req.ContentBytes()
	if err != nil {
		return SpeechToTextResponse{}, fmt.Errorf("%w: content_base64 is invalid", ErrInvalidRequest)
	}
	if len(content) == 0 {
		return SpeechToTextResponse{}, fmt.Errorf("%w: audio content is required", ErrInvalidRequest)
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
	if module.ProviderID != "" {
		return SpeechToTextResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, module.ProviderID)
	}
	return SpeechToTextResponse{}, ErrUnsupportedProvider
}

func (s *Service) whisperCppSpeechToText(ctx context.Context, module setup.VoiceModuleDescriptor, req SpeechToTextRequest, content []byte) (SpeechToTextResponse, error) {
	provider, ok := voiceProviderByID(module, "whispercpp")
	if !ok {
		return SpeechToTextResponse{}, fmt.Errorf("%w: whisper.cpp", ErrProviderUnavailable)
	}
	if err := ensureVoiceProviderAvailable(provider); err != nil {
		return SpeechToTextResponse{}, err
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

func ensureVoiceProviderAvailable(provider setup.VoiceProviderOption) error {
	if !provider.Downloaded {
		return fmt.Errorf("%w: %s is not installed", ErrProviderUnavailable, firstNonEmpty(provider.Name, provider.ID))
	}
	if strings.EqualFold(strings.TrimSpace(provider.RuntimeState), localruntime.RuntimeUnavailable) {
		detail := firstNonEmpty(provider.RuntimeDetail, firstNonEmpty(provider.Name, provider.ID)+" runtime is unavailable")
		return fmt.Errorf("%w: %s", ErrProviderUnavailable, detail)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
