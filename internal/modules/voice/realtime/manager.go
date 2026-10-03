package realtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/ids"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

type Manager struct {
	core     CoreBridge
	specs    []ProviderSpec
	keys     keyChecks
	configMu sync.RWMutex
	config   Config
	mu       sync.RWMutex
	sessions map[string]*voiceSession
	now      func() time.Time
}

type voiceSession struct {
	mu                sync.Mutex
	info              SessionInfo
	workingDir        string
	systemInstruction string
}

// NewManager serves realtime voice through specs; the first is the default.
func NewManager(coreService CoreBridge, specs ...ProviderSpec) *Manager {
	return &Manager{
		core:     coreService,
		specs:    specs,
		sessions: map[string]*voiceSession{},
		now:      time.Now,
	}
}

func (m *Manager) currentConfig() Config {
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	return m.config
}

func (m *Manager) setConfig(cfg Config) {
	m.configMu.Lock()
	m.config = cfg
	m.configMu.Unlock()
}

func (m *Manager) spec(providerID string) (ProviderSpec, bool) {
	providerID = normalizeID(providerID)
	for _, spec := range m.specs {
		if spec.ID == providerID {
			return spec, true
		}
	}
	return ProviderSpec{}, false
}

// activeSpec is the selected provider, or the default one.
func (m *Manager) activeSpec(cfg Config) ProviderSpec {
	if spec, ok := m.spec(cfg.ProviderID); ok {
		return spec
	}
	if len(m.specs) > 0 {
		return m.specs[0]
	}
	return ProviderSpec{}
}

func (m *Manager) Descriptor(ctx context.Context) ModuleDescriptor {
	cfg := m.currentConfig()
	providers := make([]ProviderDescriptor, 0, len(m.specs))
	for _, spec := range m.specs {
		providers = append(providers, m.providerDescriptor(ctx, spec, cfg.provider(spec)))
	}
	active := providerDescriptorByID(providers, m.activeSpec(cfg).ID)
	status := "Disabled"
	if cfg.Enabled {
		status = active.Status
	}
	return ModuleDescriptor{
		ID:           ModuleID,
		Title:        "Realtime Voice",
		Enabled:      cfg.Enabled,
		Ready:        cfg.Enabled && active.Configured,
		ProviderID:   active.ID,
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
	cfg := m.currentConfig()
	if !cfg.Enabled {
		return SessionInfo{}, ErrDisabled
	}
	spec := m.activeSpec(cfg)
	if req.ProviderID != "" {
		var ok bool
		if spec, ok = m.spec(req.ProviderID); !ok {
			return SessionInfo{}, fmt.Errorf("%w: %s", ErrProviderUnavailable, req.ProviderID)
		}
	}
	providerCfg := cfg.provider(spec)
	modelID := textutil.FirstNonEmpty(req.ModelID, providerCfg.ModelID)
	if modelID == "" {
		return SessionInfo{}, fmt.Errorf("%w: realtime voice model is required", ErrInvalidRequest)
	}
	voiceID := textutil.FirstNonEmpty(req.VoiceID, providerCfg.VoiceID)
	language := spec.NormalizeLanguage(textutil.FirstNonEmpty(req.Language, providerCfg.Language))
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
	persistMode := normalizePersistMode(req.PersistMode, PersistModeTurnsAndSummary)
	now := m.now().UTC()
	session := &voiceSession{
		info: SessionInfo{
			ID:            ids.New("voice"),
			Status:        SessionStatusCreated,
			ProviderID:    spec.ID,
			ProviderName:  spec.Name,
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
		workingDir:        textutil.FirstNonEmpty(req.WorkingDir, coreSession.WorkingDir),
		systemInstruction: strings.TrimSpace(req.SystemInstruction),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneUnstreamedLocked(now)
	if limit := maxSessions(cfg.MaxSessions); limit > 0 && len(m.sessions) >= limit {
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
	m.forgetSession(sessionID)
	return m.markSessionClosed(session), nil
}
