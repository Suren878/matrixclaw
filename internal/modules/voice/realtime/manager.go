package realtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type ConfigSource func(context.Context) Config

type Manager struct {
	core         CoreBridge
	config       Config
	configSource ConfigSource
	providersMu  sync.RWMutex
	providers    map[string]Provider
	mu           sync.RWMutex
	sessions     map[string]*voiceSession
	now          func() time.Time
}

type voiceSession struct {
	mu                sync.Mutex
	info              SessionInfo
	workingDir        string
	systemInstruction string
}

func NewManager(coreService CoreBridge, cfg Config, providers ...Provider) *Manager {
	m := &Manager{
		core:      coreService,
		config:    normalizeConfig(cfg),
		providers: map[string]Provider{},
		sessions:  map[string]*voiceSession{},
		now:       time.Now,
	}
	m.RegisterProvider(providers...)
	return m
}

func (m *Manager) SetConfigSource(source ConfigSource) *Manager {
	if m != nil {
		m.configSource = source
	}
	return m
}

func (m *Manager) RegisterProvider(providers ...Provider) {
	if m == nil {
		return
	}
	m.providersMu.Lock()
	defer m.providersMu.Unlock()
	if m.providers == nil {
		m.providers = map[string]Provider{}
	}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		descriptor := provider.Descriptor(context.Background())
		id := normalizeID(descriptor.ID)
		if id == "" {
			continue
		}
		m.providers[id] = provider
	}
}

func (m *Manager) Descriptor(ctx context.Context) ModuleDescriptor {
	cfg := m.currentConfig(ctx)
	providers := m.providerDescriptors(ctx)
	providerID := normalizeID(cfg.ProviderID)
	if providerID == "" && len(providers) > 0 {
		providerID = normalizeID(providers[0].ID)
	}
	active := providerDescriptorByID(providers, providerID)
	status := "Disabled"
	if cfg.Enabled {
		status = active.Status
		if strings.TrimSpace(status) == "" {
			status = "Ready"
		}
	} else if active.Configured {
		status = "Disabled"
	}
	return ModuleDescriptor{
		ID:           ModuleID,
		Title:        "Realtime Voice",
		Enabled:      cfg.Enabled,
		ProviderID:   providerID,
		ProviderName: active.Name,
		ModelID:      active.Config.ModelID,
		Status:       status,
		Config:       active.Config,
		InputAudio:   DefaultInputAudioFormat(),
		OutputAudio:  DefaultOutputAudioFormat(),
		Providers:    providers,
	}
}

func (m *Manager) CreateSession(ctx context.Context, req SessionCreateRequest) (SessionInfo, error) {
	if m == nil || m.core == nil {
		return SessionInfo{}, fmt.Errorf("%w: realtime manager is not configured", ErrProviderUnavailable)
	}
	cfg := m.currentConfig(ctx)
	if !cfg.Enabled {
		return SessionInfo{}, ErrDisabled
	}
	providerID := firstNonEmpty(req.ProviderID, cfg.ProviderID, ProviderGemini)
	provider, descriptor, ok := m.provider(ctx, providerID)
	if !ok || provider == nil {
		return SessionInfo{}, fmt.Errorf("%w: %s", ErrProviderUnavailable, providerID)
	}
	modelID := firstNonEmpty(req.ModelID, descriptor.Config.ModelID)
	if modelID == "" {
		return SessionInfo{}, fmt.Errorf("%w: realtime voice model is required", ErrInvalidRequest)
	}
	voiceID := firstNonEmpty(req.VoiceID, descriptor.Config.VoiceID)
	language := firstNonEmpty(req.Language, descriptor.Config.Language)
	inputAudio := normalizeAudioFormat(req.InputAudio, DefaultInputAudioFormat())
	outputAudio := normalizeAudioFormat(req.OutputAudio, DefaultOutputAudioFormat())
	if err := validateAudioFormat(inputAudio, DefaultInputAudioFormat(), "input_audio"); err != nil {
		return SessionInfo{}, err
	}
	if err := validateAudioFormat(outputAudio, DefaultOutputAudioFormat(), "output_audio"); err != nil {
		return SessionInfo{}, err
	}

	coreSession, err := m.resolveCoreSession(ctx, req)
	if err != nil {
		return SessionInfo{}, err
	}
	persistMode := normalizePersistMode(req.PersistMode, cfg.PersistMode)
	now := m.now().UTC()
	session := &voiceSession{
		info: SessionInfo{
			ID:            newID("voice"),
			Status:        SessionStatusCreated,
			ProviderID:    normalizeID(providerID),
			ProviderName:  descriptor.Name,
			ModelID:       modelID,
			VoiceID:       voiceID,
			Language:      language,
			CoreSessionID: coreSession.ID,
			Client:        strings.TrimSpace(req.Client),
			ExternalKey:   strings.TrimSpace(req.ExternalKey),
			PersistMode:   persistMode,
			InputAudio:    inputAudio,
			OutputAudio:   outputAudio,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		workingDir:        firstNonEmpty(req.WorkingDir, coreSession.WorkingDir),
		systemInstruction: strings.TrimSpace(req.SystemInstruction),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if limit := maxSessions(cfg.MaxSessions); limit > 0 && m.activeSessionCountLocked() >= limit {
		return SessionInfo{}, fmt.Errorf("%w: active realtime voice session limit reached", ErrInvalidRequest)
	}
	m.sessions[session.info.ID] = session
	return session.info, nil
}

func (m *Manager) Session(ctx context.Context, sessionID string) (SessionInfo, error) {
	session, ok := m.session(sessionID)
	if !ok {
		return SessionInfo{}, ErrSessionNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.info, nil
}

func (m *Manager) CloseSession(ctx context.Context, sessionID string) (SessionInfo, error) {
	session, ok := m.session(sessionID)
	if !ok {
		return SessionInfo{}, ErrSessionNotFound
	}
	return m.markSessionClosed(session, "", SessionStatusClosed), nil
}
