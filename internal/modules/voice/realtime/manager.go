package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
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

type pendingTool struct {
	ID   string
	Name string
}

type streamReadResult struct {
	event Event
	err   error
}

type providerReadResult struct {
	output ProviderOutput
	err    error
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
	_ = provider
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

func (m *Manager) ServeStream(ctx context.Context, sessionID string, stream Stream) error {
	if stream == nil {
		return fmt.Errorf("%w: stream is required", ErrInvalidRequest)
	}
	session, ok := m.session(sessionID)
	if !ok {
		return ErrSessionNotFound
	}
	info := m.markSessionStreaming(session)
	provider, _, ok := m.provider(ctx, info.ProviderID)
	if !ok || provider == nil {
		return fmt.Errorf("%w: %s", ErrProviderUnavailable, info.ProviderID)
	}
	coreSession, err := m.core.GetSession(ctx, info.CoreSessionID)
	if err != nil {
		m.markSessionClosed(session, err.Error(), SessionStatusFailed)
		return err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = stream.Close(nil) }()

	conn, err := provider.Connect(streamCtx, ProviderConnectRequest{
		VoiceSessionID:    info.ID,
		SessionID:         info.CoreSessionID,
		WorkingDir:        firstNonEmpty(session.workingDir, coreSession.WorkingDir),
		ModelID:           info.ModelID,
		VoiceID:           info.VoiceID,
		Language:          info.Language,
		SystemInstruction: m.systemInstruction(session),
		InputAudio:        info.InputAudio,
		OutputAudio:       info.OutputAudio,
		Tools:             m.toolDeclarations(),
	})
	if err != nil {
		m.markSessionClosed(session, err.Error(), SessionStatusFailed)
		_ = stream.Write(context.Background(), newEvent(info.ID, EventError, ErrorPayload{Message: err.Error(), Recoverable: false}))
		return err
	}
	defer func() { _ = conn.Close(nil) }()

	if err := stream.Write(streamCtx, newEvent(info.ID, EventSessionReady, map[string]any{"session": info})); err != nil {
		m.markSessionClosed(session, err.Error(), SessionStatusFailed)
		return err
	}

	clientCh := make(chan streamReadResult, 16)
	providerCh := make(chan providerReadResult, 16)
	coreCh := m.core.SubscribeEvents(streamCtx, info.CoreSessionID)
	go readClientEvents(streamCtx, stream, clientCh)
	go readProviderEvents(streamCtx, conn, providerCh)

	state := streamState{
		manager:     m,
		session:     session,
		info:        info,
		coreSession: coreSession,
		stream:      stream,
		provider:    conn,
		pending:     map[string]pendingTool{},
	}

	for {
		select {
		case item := <-clientCh:
			if item.err != nil {
				m.markSessionClosed(session, "", SessionStatusClosed)
				return nil
			}
			if done, err := state.handleClientEvent(streamCtx, item.event); done || err != nil {
				if err != nil {
					m.markSessionClosed(session, err.Error(), SessionStatusFailed)
					return err
				}
				m.markSessionClosed(session, "", SessionStatusClosed)
				return nil
			}
		case item := <-providerCh:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) || streamCtx.Err() != nil {
					m.markSessionClosed(session, "", SessionStatusClosed)
					return nil
				}
				m.markSessionClosed(session, item.err.Error(), SessionStatusFailed)
				_ = stream.Write(context.Background(), newEvent(info.ID, EventError, ErrorPayload{Message: item.err.Error(), Recoverable: false}))
				return item.err
			}
			if err := state.handleProviderOutput(streamCtx, item.output); err != nil {
				m.markSessionClosed(session, err.Error(), SessionStatusFailed)
				return err
			}
		case event := <-coreCh:
			if err := state.handleCoreEvent(streamCtx, event); err != nil {
				m.markSessionClosed(session, err.Error(), SessionStatusFailed)
				return err
			}
		case <-streamCtx.Done():
			m.markSessionClosed(session, "", SessionStatusClosed)
			return nil
		}
	}
}

type streamState struct {
	manager             *Manager
	session             *voiceSession
	info                SessionInfo
	coreSession         core.Session
	stream              Stream
	provider            ProviderConnection
	pending             map[string]pendingTool
	inputTranscript     strings.Builder
	assistantTranscript strings.Builder
}

func (s *streamState) handleClientEvent(ctx context.Context, event Event) (bool, error) {
	switch event.Type {
	case EventInputAudioAppend:
		var payload InputAudioPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return false, err
		}
		payload.AudioBase64 = strings.TrimSpace(payload.AudioBase64)
		if payload.AudioBase64 == "" {
			return false, fmt.Errorf("%w: audio_base64 is required", ErrInvalidRequest)
		}
		return false, s.provider.Send(ctx, ProviderInput{
			Type:          ProviderInputAudioAppend,
			AudioBase64:   payload.AudioBase64,
			AudioMIMEType: firstNonEmpty(payload.MIMEType, audioMIMEType(s.info.InputAudio)),
		})
	case EventInputAudioEnd:
		return false, s.provider.Send(ctx, ProviderInput{Type: ProviderInputAudioEnd})
	case EventInputTextAppend:
		var payload InputTextPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return false, err
		}
		text := strings.TrimSpace(payload.Text)
		if text == "" {
			return false, fmt.Errorf("%w: text is required", ErrInvalidRequest)
		}
		return false, s.provider.Send(ctx, ProviderInput{Type: ProviderInputTextAppend, Text: text, EndOfTurn: payload.EndOfTurn})
	case EventResponseCancel:
		return false, s.provider.Send(ctx, ProviderInput{Type: ProviderInputCancel})
	case EventSessionClose:
		return true, nil
	default:
		return false, fmt.Errorf("%w: unsupported event type %q", ErrInvalidRequest, event.Type)
	}
}

func (s *streamState) handleProviderOutput(ctx context.Context, output ProviderOutput) error {
	switch output.Type {
	case ProviderOutputInputTranscript:
		text := strings.TrimSpace(output.Text)
		if text == "" {
			return nil
		}
		appendTranscript(&s.inputTranscript, text)
		return s.stream.Write(ctx, newEvent(s.info.ID, EventInputTranscriptDelta, TranscriptPayload{Text: text}))
	case ProviderOutputAssistantTranscript:
		text := strings.TrimSpace(output.Text)
		if text == "" {
			return nil
		}
		appendTranscript(&s.assistantTranscript, text)
		return s.stream.Write(ctx, newEvent(s.info.ID, EventAssistantTranscriptDelta, TranscriptPayload{Text: text}))
	case ProviderOutputAssistantAudio:
		if strings.TrimSpace(output.AudioBase64) == "" {
			return nil
		}
		return s.stream.Write(ctx, newEvent(s.info.ID, EventAssistantAudioDelta, AssistantAudioPayload{
			AudioBase64: output.AudioBase64,
			MIMEType:    firstNonEmpty(output.MIMEType, audioMIMEType(s.info.OutputAudio)),
		}))
	case ProviderOutputTurnComplete:
		return s.finishTurn(ctx)
	case ProviderOutputToolCall:
		return s.handleToolCalls(ctx, output.ToolCalls)
	case ProviderOutputGoAway:
		return s.stream.Write(ctx, newEvent(s.info.ID, EventBackpressure, map[string]any{"message": "provider will close the realtime session soon"}))
	case ProviderOutputSessionResumption:
		return nil
	case ProviderOutputInterrupted:
		s.assistantTranscript.Reset()
		return s.stream.Write(ctx, newEvent(s.info.ID, EventInterrupted, map[string]any{}))
	case ProviderOutputError:
		return s.stream.Write(ctx, newEvent(s.info.ID, EventError, ErrorPayload{Message: output.Error, Recoverable: true}))
	default:
		return nil
	}
}

func (s *streamState) handleToolCalls(ctx context.Context, calls []ProviderToolCall) error {
	for i := range calls {
		call := calls[i]
		call.ID = firstNonEmpty(call.ID, fmt.Sprintf("%s_tool_%d", s.info.ID, i+1))
		call.Name = strings.TrimSpace(call.Name)
		if call.Name == "" {
			continue
		}
		if len(call.Args) == 0 {
			call.Args = json.RawMessage(`{}`)
		}
		if err := s.stream.Write(ctx, newEvent(s.info.ID, EventToolCall, ToolCallPayload{ID: call.ID, Name: call.Name, Args: call.Args})); err != nil {
			return err
		}
		result, err := s.manager.core.ExecuteTool(ctx, core.ExecuteToolInput{
			SessionID:   s.info.CoreSessionID,
			ToolName:    call.Name,
			ToolCallID:  call.ID,
			WorkingDir:  firstNonEmpty(s.session.workingDir, s.coreSession.WorkingDir),
			Client:      s.info.Client,
			ExternalKey: s.info.ExternalKey,
			Args:        call.Args,
		})
		if result.Approval != nil {
			s.pending[call.ID] = pendingTool{ID: call.ID, Name: call.Name}
			if err := s.stream.Write(ctx, newEvent(s.info.ID, EventApprovalRequested, ApprovalRequestedPayload{
				ID:          result.Approval.ID,
				ToolCallID:  call.ID,
				ToolName:    call.Name,
				Action:      result.Approval.Action,
				Path:        result.Approval.Path,
				Description: result.Approval.Description,
			})); err != nil {
				return err
			}
			continue
		}
		content, isError := toolResultContent(result, err)
		if err := s.sendProviderToolResult(ctx, call.ID, call.Name, content, isError); err != nil {
			return err
		}
		if err := s.stream.Write(ctx, newEvent(s.info.ID, EventToolResult, ToolResultPayload{ID: call.ID, Name: call.Name, Content: content, IsError: isError})); err != nil {
			return err
		}
	}
	return nil
}

func (s *streamState) handleCoreEvent(ctx context.Context, event core.Event) error {
	switch event.Type {
	case core.EventApprovalResult:
		var payload core.PermissionNotification
		if !payloadAs(event.Payload, &payload) {
			return nil
		}
		pending, ok := s.pending[payload.ToolCallID]
		if !ok {
			return nil
		}
		if err := s.stream.Write(ctx, newEvent(s.info.ID, EventApprovalResolved, ApprovalResolvedPayload{
			ID:         payload.ApprovalID,
			ToolCallID: payload.ToolCallID,
			Granted:    payload.Granted,
			Denied:     payload.Denied,
		})); err != nil {
			return err
		}
		if payload.Denied {
			delete(s.pending, payload.ToolCallID)
			if err := s.sendProviderToolResult(ctx, pending.ID, pending.Name, "approval denied", true); err != nil {
				return err
			}
		}
	case core.EventMessageCreated:
		var message core.Message
		if !payloadAs(event.Payload, &message) || message.Role != core.MessageRoleTool {
			return nil
		}
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			pending, ok := s.pending[part.ToolResult.ToolCallID]
			if !ok {
				continue
			}
			delete(s.pending, part.ToolResult.ToolCallID)
			content := strings.TrimSpace(part.ToolResult.Content)
			if content == "" {
				content = strings.TrimSpace(message.Content)
			}
			if err := s.sendProviderToolResult(ctx, pending.ID, pending.Name, content, part.ToolResult.IsError); err != nil {
				return err
			}
			if err := s.stream.Write(ctx, newEvent(s.info.ID, EventToolResult, ToolResultPayload{
				ID:      pending.ID,
				Name:    pending.Name,
				Content: content,
				IsError: part.ToolResult.IsError,
			})); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *streamState) finishTurn(ctx context.Context) error {
	input := strings.TrimSpace(s.inputTranscript.String())
	assistant := strings.TrimSpace(s.assistantTranscript.String())
	if err := s.stream.Write(ctx, newEvent(s.info.ID, EventInputTranscriptFinal, TranscriptPayload{Text: input, Final: true})); err != nil {
		return err
	}
	if err := s.stream.Write(ctx, newEvent(s.info.ID, EventAssistantTranscriptFinal, TranscriptPayload{Text: assistant, Final: true})); err != nil {
		return err
	}
	if err := s.stream.Write(ctx, newEvent(s.info.ID, EventTurnFinal, TurnFinalPayload{
		InputTranscript:     input,
		AssistantTranscript: assistant,
	})); err != nil {
		return err
	}
	if s.info.PersistMode != PersistModeNone && (input != "" || assistant != "") {
		if _, err := s.manager.core.CommitRealtimeVoiceTurn(ctx, core.CommitRealtimeVoiceTurnInput{
			SessionID:           s.info.CoreSessionID,
			ProviderID:          s.info.ProviderID,
			ModelID:             s.info.ModelID,
			UserTranscript:      input,
			AssistantTranscript: assistant,
		}); err != nil {
			return err
		}
	}
	s.inputTranscript.Reset()
	s.assistantTranscript.Reset()
	return nil
}

func (s *streamState) sendProviderToolResult(ctx context.Context, id string, name string, content string, isError bool) error {
	response := map[string]any{
		"content":  strings.TrimSpace(content),
		"is_error": isError,
	}
	if response["content"] == "" {
		response["content"] = "ok"
	}
	return s.provider.Send(ctx, ProviderInput{
		Type: ProviderInputToolResult,
		ToolResponses: []ProviderToolResponse{{
			ID:       id,
			Name:     name,
			Response: response,
		}},
	})
}

func (m *Manager) resolveCoreSession(ctx context.Context, req SessionCreateRequest) (core.Session, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" {
		session, err := m.core.GetSession(ctx, sessionID)
		if err != nil {
			return core.Session{}, err
		}
		if strings.TrimSpace(req.Client) != "" && strings.TrimSpace(req.ExternalKey) != "" {
			if _, err := m.core.UseBinding(ctx, core.UseBindingInput{Client: req.Client, ExternalKey: req.ExternalKey, SessionID: session.ID}); err != nil {
				return core.Session{}, err
			}
		}
		return session, nil
	}
	if strings.TrimSpace(req.Client) != "" && strings.TrimSpace(req.ExternalKey) != "" {
		binding, err := m.core.CurrentBinding(ctx, req.Client, req.ExternalKey)
		if err == nil {
			return m.core.GetSession(ctx, binding.SessionID)
		}
		if !errors.Is(err, core.ErrBindingNotFound) && !errors.Is(err, core.ErrNotFound) {
			return core.Session{}, err
		}
	}
	session, err := m.core.CreateSession(ctx, core.CreateSessionInput{
		Title:      "Voice conversation",
		WorkingDir: req.WorkingDir,
	})
	if err != nil {
		return core.Session{}, err
	}
	if strings.TrimSpace(req.Client) != "" && strings.TrimSpace(req.ExternalKey) != "" {
		if _, err := m.core.UseBinding(ctx, core.UseBindingInput{Client: req.Client, ExternalKey: req.ExternalKey, SessionID: session.ID}); err != nil {
			return core.Session{}, err
		}
	}
	return session, nil
}

func (m *Manager) session(sessionID string) (*voiceSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[strings.TrimSpace(sessionID)]
	return session, ok
}

func (m *Manager) provider(ctx context.Context, providerID string) (Provider, ProviderDescriptor, bool) {
	providerID = normalizeID(providerID)
	m.providersMu.RLock()
	provider, ok := m.providers[providerID]
	m.providersMu.RUnlock()
	if !ok {
		return nil, ProviderDescriptor{}, false
	}
	return provider, provider.Descriptor(ctx), true
}

func (m *Manager) providerDescriptors(ctx context.Context) []ProviderDescriptor {
	m.providersMu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, provider := range m.providers {
		providers = append(providers, provider)
	}
	m.providersMu.RUnlock()
	out := make([]ProviderDescriptor, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Descriptor(ctx))
	}
	return out
}

func (m *Manager) markSessionStreaming(session *voiceSession) SessionInfo {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.info.Status != SessionStatusStreaming {
		session.info.Status = SessionStatusStreaming
		session.info.UpdatedAt = m.now().UTC()
	}
	return session.info
}

func (m *Manager) markSessionClosed(session *voiceSession, message string, status SessionStatus) SessionInfo {
	session.mu.Lock()
	defer session.mu.Unlock()
	now := m.now().UTC()
	session.info.Status = status
	session.info.Error = strings.TrimSpace(message)
	session.info.UpdatedAt = now
	session.info.ClosedAt = &now
	return session.info
}

func (m *Manager) activeSessionCountLocked() int {
	count := 0
	for _, session := range m.sessions {
		session.mu.Lock()
		status := session.info.Status
		session.mu.Unlock()
		if status != SessionStatusClosed && status != SessionStatusFailed {
			count++
		}
	}
	return count
}

func (m *Manager) currentConfig(ctx context.Context) Config {
	if m == nil {
		return Config{}
	}
	if m.configSource != nil {
		return normalizeConfig(m.configSource(ctx))
	}
	return normalizeConfig(m.config)
}

func (m *Manager) systemInstruction(session *voiceSession) string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return strings.TrimSpace(session.systemInstruction)
}

func (m *Manager) toolDeclarations() []ToolDeclaration {
	if m == nil || m.core == nil {
		return nil
	}
	specs := m.core.ListToolSpecs()
	out := make([]ToolDeclaration, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.ID)
		if name == "" {
			continue
		}
		out = append(out, ToolDeclaration{
			Name:             name,
			Description:      strings.TrimSpace(spec.Description),
			Parameters:       spec.InputJSONSchema,
			RequiresApproval: spec.RequiresApproval(),
		})
	}
	return out
}

func readClientEvents(ctx context.Context, stream Stream, out chan<- streamReadResult) {
	for {
		event, err := stream.Read(ctx)
		select {
		case out <- streamReadResult{event: event, err: err}:
		case <-ctx.Done():
		}
		if err != nil || ctx.Err() != nil {
			return
		}
	}
}

func readProviderEvents(ctx context.Context, provider ProviderConnection, out chan<- providerReadResult) {
	for {
		event, err := provider.Receive(ctx)
		select {
		case out <- providerReadResult{output: event, err: err}:
		case <-ctx.Done():
		}
		if err != nil || ctx.Err() != nil {
			return
		}
	}
}

func DefaultInputAudioFormat() AudioFormat {
	return AudioFormat{Encoding: "pcm_s16le", SampleRateHz: 16000, Channels: 1}
}

func DefaultOutputAudioFormat() AudioFormat {
	return AudioFormat{Encoding: "pcm_s16le", SampleRateHz: 24000, Channels: 1}
}

func normalizeAudioFormat(format AudioFormat, fallback AudioFormat) AudioFormat {
	format.Encoding = strings.ToLower(strings.TrimSpace(format.Encoding))
	if format.Encoding == "" {
		format.Encoding = fallback.Encoding
	}
	if format.SampleRateHz <= 0 {
		format.SampleRateHz = fallback.SampleRateHz
	}
	if format.Channels <= 0 {
		format.Channels = fallback.Channels
	}
	return format
}

func validateAudioFormat(format AudioFormat, expected AudioFormat, field string) error {
	if format != expected {
		return fmt.Errorf("%w: %s must be %s %dHz %dch", ErrInvalidRequest, field, expected.Encoding, expected.SampleRateHz, expected.Channels)
	}
	return nil
}

func audioMIMEType(format AudioFormat) string {
	if strings.EqualFold(format.Encoding, "pcm_s16le") {
		return fmt.Sprintf("audio/pcm;rate=%d", format.SampleRateHz)
	}
	return "application/octet-stream"
}

func normalizeConfig(cfg Config) Config {
	cfg.ProviderID = normalizeID(cfg.ProviderID)
	if cfg.ProviderID == "" {
		cfg.ProviderID = ProviderGemini
	}
	cfg.PersistMode = normalizePersistMode(cfg.PersistMode, PersistModeTurnsAndSummary)
	return cfg
}

func normalizePersistMode(value PersistMode, fallback PersistMode) PersistMode {
	switch PersistMode(strings.ToLower(strings.TrimSpace(string(value)))) {
	case PersistModeNone:
		return PersistModeNone
	case PersistModeTurnsAndSummary:
		return PersistModeTurnsAndSummary
	default:
		if fallback == "" {
			return PersistModeTurnsAndSummary
		}
		return fallback
	}
}

func maxSessions(value int) int {
	if value < 0 {
		return 0
	}
	if value == 0 {
		return 8
	}
	return value
}

func providerDescriptorByID(providers []ProviderDescriptor, id string) ProviderDescriptor {
	id = normalizeID(id)
	for _, provider := range providers {
		if normalizeID(provider.ID) == id {
			return provider
		}
	}
	if len(providers) > 0 {
		return providers[0]
	}
	return ProviderDescriptor{}
}

func newEvent(sessionID string, eventType EventType, payload any) Event {
	raw, _ := json.Marshal(payload)
	return Event{
		V:              ProtocolVersion,
		Type:           eventType,
		VoiceSessionID: strings.TrimSpace(sessionID),
		Payload:        raw,
		At:             time.Now().UTC(),
	}
}

func decodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: invalid event payload", ErrInvalidRequest)
	}
	return nil
}

func payloadAs(payload any, target any) bool {
	if payload == nil {
		return false
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(body, target) == nil
}

func toolResultContent(result core.ExecuteToolResult, execErr error) (string, bool) {
	if execErr != nil {
		return execErr.Error(), true
	}
	if result.ToolResultMessage == nil {
		return "ok", false
	}
	content := strings.TrimSpace(result.ToolResultMessage.Content)
	isError := false
	for _, part := range result.ToolResultMessage.Parts {
		if part.ToolResult == nil {
			continue
		}
		if strings.TrimSpace(part.ToolResult.Content) != "" {
			content = strings.TrimSpace(part.ToolResult.Content)
		}
		isError = part.ToolResult.IsError
	}
	if content == "" {
		content = "ok"
	}
	return content, isError
}

func appendTranscript(builder *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	builder.WriteString(text)
}

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return strings.TrimSpace(prefix) + "_" + hex.EncodeToString(buf)
}
