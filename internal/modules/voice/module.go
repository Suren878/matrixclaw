// Package voice is the local text-to-speech and speech-to-text modules.
package voice

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/procsup"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
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

// Module is the TTS or the STT module; both run on the daemon's local runtime.
type Module struct {
	id      string // setup.VoiceModuleTTS or setup.VoiceModuleSTT
	runtime *localruntime.Runtime
	mu      sync.RWMutex
	module  setup.VoiceModuleDescriptor
	applied bool
}

func New(id string, runtime *localruntime.Runtime) *Module {
	return &Module{id: id, runtime: runtime}
}

func (m *Module) ID() string { return m.id }

// Apply stores the module's settings and, when they changed, starts the
// selected always-running runtime in the background and stops the others.
func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	var next setup.VoiceModuleDescriptor
	for _, module := range setup.VoiceModuleDescriptors(cfg.Modules) {
		if module.ID == m.id {
			next = module
		}
	}
	m.mu.Lock()
	changed := !m.applied || !reflect.DeepEqual(m.module, next)
	m.module, m.applied = next, true
	m.mu.Unlock()
	if changed {
		m.reconcileRuntimes(next)
	}
	return nil
}

func (m *Module) reconcileRuntimes(module setup.VoiceModuleDescriptor) {
	for _, provider := range module.Providers {
		if !provider.Local {
			continue
		}
		if !module.Enabled || provider.ID != module.ProviderID || provider.Config.RuntimeMode != "always_running" {
			m.runtime.StopVoiceRuntime(provider.ID)
			continue
		}
		safego.Go("voice.autostart", func() {
			if _, err := m.runtime.ApplyVoiceAction(context.Background(), module.ID, provider, setup.VoiceProviderActionRequest{Action: localruntime.ActionStart}); err != nil {
				log.Printf("%s %s runtime autostart failed: %s", module.ID, provider.ID, err)
			}
		})
	}
}

func (m *Module) Tools() []tools.Executor {
	if m.id != setup.VoiceModuleTTS || !m.settings().Enabled {
		return nil
	}
	return []tools.Executor{&textToSpeechTool{module: m}}
}

func (m *Module) Context() string { return "" }

func (m *Module) Close() error { return nil }

func (m *Module) settings() setup.VoiceModuleDescriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.module
}

// Descriptor is the module with the local runtime's install and run state.
func (m *Module) Descriptor() setup.VoiceModuleDescriptor {
	return m.runtime.DecorateVoiceModules([]setup.VoiceModuleDescriptor{m.settings()})[0]
}

func (m *Module) Status(context.Context) modules.Status {
	module := m.Descriptor()
	provider, _ := providerByID(module, module.ProviderID)
	installed := 0
	for _, model := range provider.Models {
		if model.Installed {
			installed++
		}
	}
	return modules.Status{
		ID:      m.id,
		Title:   module.Title,
		Enabled: module.Enabled,
		Ready:   module.Enabled && ensureAvailable(provider) == nil,
		State:   module.Status,
		Detail:  provider.RuntimeDetail,
		Facts: []modules.Fact{
			{Key: "provider", Label: "Provider", Value: cmp.Or(module.ProviderName, module.ProviderID)},
			{Key: "mode", Label: "Run mode", Value: cmp.Or(provider.Config.RuntimeMode, "per_task")},
			{Key: "installed_models", Label: "Installed models", Value: strconv.Itoa(installed)},
		},
	}
}

// Action runs a provider action (download, install, start, stop...).
func (m *Module) Action(ctx context.Context, providerID string, request setup.VoiceProviderActionRequest) (setup.VoiceProviderOption, error) {
	provider, ok := providerByID(m.settings(), providerID)
	if !ok {
		return setup.VoiceProviderOption{}, fmt.Errorf("%w: %s", ErrProviderUnavailable, providerID)
	}
	return m.runtime.ApplyVoiceAction(ctx, m.id, provider, request)
}

func (m *Module) TextToSpeech(ctx context.Context, req TextToSpeechRequest) (TextToSpeechResponse, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return TextToSpeechResponse{}, fmt.Errorf("%w: text is required", ErrInvalidRequest)
	}
	module, provider, err := m.activeProvider()
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	var content []byte
	switch module.ProviderID {
	case "piper":
		content, err = m.runtime.PiperTextToSpeech(ctx, provider, text)
	case "supertonic":
		content, err = m.runtime.SupertonicTextToSpeech(ctx, provider, text)
	default:
		return TextToSpeechResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, module.ProviderID)
	}
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	mp3, err := wavToMP3(ctx, content)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	return NewTextToSpeechResponse(mp3, defaultTTSMIME, defaultTTSName), nil
}

func (m *Module) SpeechToText(ctx context.Context, req SpeechToTextRequest) (SpeechToTextResponse, error) {
	content, err := req.ContentBytes()
	if err != nil {
		return SpeechToTextResponse{}, fmt.Errorf("%w: content_base64 is invalid", ErrInvalidRequest)
	}
	if len(content) == 0 {
		return SpeechToTextResponse{}, fmt.Errorf("%w: audio content is required", ErrInvalidRequest)
	}
	module, provider, err := m.activeProvider()
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	if module.ProviderID != "whispercpp" {
		return SpeechToTextResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, module.ProviderID)
	}
	text, err := m.runtime.WhisperSpeechToText(ctx, provider, localruntime.WhisperSpeechInput{
		Content:  content,
		FileName: req.FileName,
		MIMEType: req.MIMEType,
		Language: cmp.Or(strings.TrimSpace(req.Language), strings.TrimSpace(provider.Config.Language)),
	})
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	return SpeechToTextResponse{Text: strings.TrimSpace(text)}, nil
}

// activeProvider is the selected provider when the module is on and it can run.
func (m *Module) activeProvider() (setup.VoiceModuleDescriptor, setup.VoiceProviderOption, error) {
	module := m.Descriptor()
	if !module.Enabled {
		return module, setup.VoiceProviderOption{}, ErrModuleDisabled
	}
	provider, ok := providerByID(module, module.ProviderID)
	if !ok {
		return module, provider, fmt.Errorf("%w: %s", ErrProviderUnavailable, module.ProviderID)
	}
	return module, provider, ensureAvailable(provider)
}

func ensureAvailable(provider setup.VoiceProviderOption) error {
	name := cmp.Or(provider.Name, provider.ID)
	if !provider.Downloaded {
		return fmt.Errorf("%w: %s is not installed", ErrProviderUnavailable, name)
	}
	if provider.RuntimeState == localruntime.RuntimeUnavailable {
		return fmt.Errorf("%w: %s", ErrProviderUnavailable, cmp.Or(provider.RuntimeDetail, name+" runtime is unavailable"))
	}
	return nil
}

func providerByID(module setup.VoiceModuleDescriptor, providerID string) (setup.VoiceProviderOption, bool) {
	for _, provider := range module.Providers {
		if strings.EqualFold(provider.ID, providerID) {
			return provider, true
		}
	}
	return setup.VoiceProviderOption{}, false
}

func wavToMP3(ctx context.Context, content []byte) ([]byte, error) {
	if len(content) == 0 {
		return nil, errors.New("audio content is empty")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "wav", "-i", "pipe:0", "-codec:a", "libmp3lame", "-b:a", "96k", "-f", "mp3", "pipe:1")
	procsup.Prepare(cmd)
	cmd.Stdin = bytes.NewReader(content)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("ffmpeg is required to convert local TTS audio to MP3")
		}
		return nil, fmt.Errorf("convert local TTS audio to MP3: %s", cmp.Or(strings.TrimSpace(stderr.String()), err.Error()))
	}
	if stdout.Len() == 0 {
		return nil, errors.New("ffmpeg returned empty MP3 audio")
	}
	return stdout.Bytes(), nil
}
