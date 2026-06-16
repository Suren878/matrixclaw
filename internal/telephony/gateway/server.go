package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	cfg    Config
	ari    *ariClient
	events *ariEventHub
	mu     sync.RWMutex
	calls  map[string]*Call
	client *http.Client
}

type Call struct {
	ID                         string     `json:"id"`
	Direction                  string     `json:"direction,omitempty"`
	To                         string     `json:"to"`
	From                       string     `json:"from,omitempty"`
	Profile                    string     `json:"profile,omitempty"`
	Objective                  string     `json:"objective,omitempty"`
	Status                     string     `json:"status"`
	Error                      string     `json:"error,omitempty"`
	RealtimeSessionID          string     `json:"realtime_session_id,omitempty"`
	CoreSessionID              string     `json:"session_id,omitempty"`
	OriginClient               string     `json:"origin_client,omitempty"`
	OriginExternalKey          string     `json:"origin_external_key,omitempty"`
	OriginSessionID            string     `json:"origin_session_id,omitempty"`
	BridgeID                   string     `json:"bridge_id,omitempty"`
	ChannelID                  string     `json:"channel_id,omitempty"`
	ExternalChannelID          string     `json:"external_channel_id,omitempty"`
	CaptureBridgeID            string     `json:"capture_bridge_id,omitempty"`
	PlaybackBridgeID           string     `json:"playback_bridge_id,omitempty"`
	CaptureSnoopChannelID      string     `json:"capture_snoop_channel_id,omitempty"`
	PlaybackSnoopChannelID     string     `json:"playback_snoop_channel_id,omitempty"`
	CaptureExternalChannelID   string     `json:"capture_external_channel_id,omitempty"`
	PlaybackExternalChannelID  string     `json:"playback_external_channel_id,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
	AnsweredAt                 *time.Time `json:"answered_at,omitempty"`
	FinishedAt                 *time.Time `json:"finished_at,omitempty"`
	cancel                     context.CancelFunc
	transcriptMu               sync.Mutex
	InputTranscript            string               `json:"input_transcript,omitempty"`
	AssistantTranscript        string               `json:"assistant_transcript,omitempty"`
	Transcript                 []CallTranscriptTurn `json:"transcript,omitempty"`
	Recording                  *CallRecording       `json:"recording,omitempty"`
	RTP                        rtpStats             `json:"rtp,omitempty"`
	RTPCapture                 rtpStats             `json:"rtp_capture,omitempty"`
	RTPPlayback                rtpStats             `json:"rtp_playback,omitempty"`
	rtp                        *rtpSession
	rtpIn                      *rtpSession
	rtpOut                     *rtpSession
	currentInputTranscript     string
	currentAssistantTranscript string
}

type CallSnapshot struct {
	ID                        string               `json:"id"`
	Direction                 string               `json:"direction,omitempty"`
	To                        string               `json:"to"`
	From                      string               `json:"from,omitempty"`
	Profile                   string               `json:"profile,omitempty"`
	Objective                 string               `json:"objective,omitempty"`
	Status                    string               `json:"status"`
	Error                     string               `json:"error,omitempty"`
	RealtimeSessionID         string               `json:"realtime_session_id,omitempty"`
	CoreSessionID             string               `json:"session_id,omitempty"`
	OriginClient              string               `json:"origin_client,omitempty"`
	OriginExternalKey         string               `json:"origin_external_key,omitempty"`
	OriginSessionID           string               `json:"origin_session_id,omitempty"`
	BridgeID                  string               `json:"bridge_id,omitempty"`
	ChannelID                 string               `json:"channel_id,omitempty"`
	ExternalChannelID         string               `json:"external_channel_id,omitempty"`
	CaptureBridgeID           string               `json:"capture_bridge_id,omitempty"`
	PlaybackBridgeID          string               `json:"playback_bridge_id,omitempty"`
	CaptureSnoopChannelID     string               `json:"capture_snoop_channel_id,omitempty"`
	PlaybackSnoopChannelID    string               `json:"playback_snoop_channel_id,omitempty"`
	CaptureExternalChannelID  string               `json:"capture_external_channel_id,omitempty"`
	PlaybackExternalChannelID string               `json:"playback_external_channel_id,omitempty"`
	CreatedAt                 time.Time            `json:"created_at"`
	UpdatedAt                 time.Time            `json:"updated_at"`
	AnsweredAt                *time.Time           `json:"answered_at,omitempty"`
	FinishedAt                *time.Time           `json:"finished_at,omitempty"`
	InputTranscript           string               `json:"input_transcript,omitempty"`
	AssistantTranscript       string               `json:"assistant_transcript,omitempty"`
	Transcript                []CallTranscriptTurn `json:"transcript,omitempty"`
	Recording                 *CallRecording       `json:"recording,omitempty"`
	RTP                       rtpStats             `json:"rtp,omitempty"`
	RTPCapture                rtpStats             `json:"rtp_capture,omitempty"`
	RTPPlayback               rtpStats             `json:"rtp_playback,omitempty"`
}

type createCallRequest struct {
	To                          string `json:"to"`
	Profile                     string `json:"profile,omitempty"`
	Objective                   string `json:"objective,omitempty"`
	SystemInstruction           string `json:"system_instruction,omitempty"`
	InitialMessage              string `json:"initial_message,omitempty"`
	AssistantName               string `json:"assistant_name,omitempty"`
	ExternalKey                 string `json:"external_key,omitempty"`
	SessionID                   string `json:"session_id,omitempty"`
	OriginClient                string `json:"origin_client,omitempty"`
	OriginExternalKey           string `json:"origin_external_key,omitempty"`
	OriginSessionID             string `json:"origin_session_id,omitempty"`
	PhonePrompt                 string `json:"phone_prompt,omitempty"`
	AssistantCustomInstructions string `json:"assistant_custom_instructions,omitempty"`
	PostCallReport              *bool  `json:"post_call_report,omitempty"`
}

type CallTranscriptTurn struct {
	At        time.Time `json:"at,omitempty"`
	Speaker   string    `json:"speaker"`
	Text      string    `json:"text"`
	TurnIndex int       `json:"turn_index,omitempty"`
}

func Run(ctx context.Context, cfg Config) error {
	s := NewServer(cfg)
	if cfg.ARIPassword != "" {
		s.events.Start(ctx)
		go s.cleanupStaleARIOnReady(ctx)
	}
	if cfg.InboundEnabled {
		go s.runInboundListener(ctx)
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("matrixclaw telephony gateway listening on %s", cfg.HTTPAddr)
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func NewServer(cfg Config) *Server {
	ari := newARIClient(cfg.ARIURL, cfg.ARIUser, cfg.ARIPassword)
	return &Server{
		cfg:    cfg,
		ari:    ari,
		events: newARIEventHub(ari, cfg.ARIApp),
		calls:  map[string]*Call{},
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/calls", s.handleCalls)
	mux.HandleFunc("/v1/calls/", s.handleCallByID)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(s.cfg.GatewayToken)
		if token != "" && bearerToken(r.Header.Get("Authorization")) != token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	status := "ready"
	var problems []string
	if s.cfg.ARIPassword == "" {
		status = "not_ready"
		problems = append(problems, "ARI password is required")
	}
	if s.cfg.MatrixclawToken == "" {
		status = "not_ready"
		problems = append(problems, "MatrixClaw API token is required")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	if s.cfg.ARIPassword != "" {
		if err := s.ari.probe(ctx); err != nil {
			status = "not_ready"
			problems = append(problems, "ARI: "+err.Error())
		}
	}
	if s.cfg.MatrixclawToken != "" {
		if err := s.probeMatrixclaw(ctx); err != nil {
			status = "not_ready"
			problems = append(problems, "MatrixClaw: "+err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                 status,
		"error":                  strings.Join(problems, "; "),
		"ari_app":                s.cfg.ARIApp,
		"profile":                s.cfg.SIPProfile,
		"rtp_bind":               s.cfg.RTPBind,
		"inbound_enabled":        s.cfg.InboundEnabled,
		"inbound_allowed":        len(s.cfg.InboundAllowed),
		"record_calls":           s.cfg.RecordCalls,
		"recording_format":       s.cfg.RecordingFormat,
		"recording_temp_storage": s.cfg.RecordingStorage,
	})
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		calls := make([]CallSnapshot, 0, len(s.calls))
		for _, call := range s.calls {
			s.syncCallStats(call)
			calls = append(calls, callSnapshot(call))
		}
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"calls": calls})
	case http.MethodPost:
		var req createCallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		call, err := s.startCall(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"call": call})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleCallByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/calls/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
		return
	}
	call, ok := s.call(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.syncCallStats(call)
		writeJSON(w, http.StatusOK, map[string]any{"call": callSnapshot(call)})
	case http.MethodDelete:
		call.cancel()
		writeJSON(w, http.StatusOK, map[string]any{"call": callSnapshot(call)})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) startCall(parent context.Context, req createCallRequest) (CallSnapshot, error) {
	to := normalizePhone(req.To)
	if to == "" {
		return CallSnapshot{}, errors.New("to is required")
	}
	if s.cfg.ARIPassword == "" {
		return CallSnapshot{}, errors.New("ARI password is required")
	}
	if s.cfg.MatrixclawToken == "" {
		return CallSnapshot{}, errors.New("MatrixClaw API token is required")
	}
	id := newID("call")
	ctx, cancel := context.WithCancel(context.Background())
	if s.cfg.MaxCallDuration > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.cfg.MaxCallDuration)
	}
	now := time.Now().UTC()
	call := &Call{
		ID:                id,
		Direction:         "outbound",
		To:                to,
		Profile:           firstNonEmpty(req.Profile, s.cfg.SIPProfile),
		Objective:         firstNonEmpty(req.Objective, req.SystemInstruction),
		Status:            "queued",
		CreatedAt:         now,
		UpdatedAt:         now,
		OriginClient:      strings.TrimSpace(req.OriginClient),
		OriginExternalKey: strings.TrimSpace(req.OriginExternalKey),
		OriginSessionID:   strings.TrimSpace(req.OriginSessionID),
		cancel:            cancel,
	}
	s.mu.Lock()
	s.calls[id] = call
	s.mu.Unlock()
	go s.runCall(ctx, call, req)
	return callSnapshot(call), nil
}

func (s *Server) runCall(ctx context.Context, call *Call, req createCallRequest) {
	defer call.cancel()
	defer s.postCallReport(context.Background(), call, req)
	if err := s.runCallOnce(ctx, call, req); err != nil {
		s.updateCall(call, "failed", err.Error())
		log.Printf("telephony call %s failed: %v", call.ID, err)
	}
}

func (s *Server) runInboundListener(ctx context.Context) {
	if s.cfg.ARIPassword == "" || s.cfg.MatrixclawToken == "" {
		log.Printf("telephony inbound listener disabled until ARI and MatrixClaw credentials are configured")
		return
	}
	events, unsubscribe := s.events.Subscribe(256)
	defer unsubscribe()
	if err := s.events.WaitReady(ctx); err != nil {
		return
	}
	log.Printf("telephony inbound listener ready for ARI app %s", s.cfg.ARIApp)
	for {
		select {
		case event := <-events:
			if s.isInboundStart(event) {
				s.startInboundCall(ctx, event)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) isInboundStart(event ariEvent) bool {
	if event.Type != "StasisStart" || event.Channel == nil || strings.TrimSpace(event.Channel.ID) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(event.Channel.Dialplan.Exten), "h") {
		return false
	}
	if len(event.Args) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(event.Args[0]), "inbound")
}

func (s *Server) startInboundCall(parent context.Context, event ariEvent) {
	channel := event.Channel
	if channel == nil {
		return
	}
	from := firstNonEmpty(channel.Caller.Number, channel.Caller.Name, channel.Name)
	if !s.cfg.InboundCallerAllowed(from) {
		log.Printf("telephony inbound call rejected from %q (%s)", strings.TrimSpace(from), ariChannelSummary(channel))
		_ = s.ari.hangup(context.Background(), channel.ID)
		return
	}
	log.Printf("telephony inbound call accepted from %q (%s)", strings.TrimSpace(from), ariChannelSummary(channel))
	id := newID("call")
	ctx, cancel := context.WithCancel(context.Background())
	if s.cfg.MaxCallDuration > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.cfg.MaxCallDuration)
	}
	now := time.Now().UTC()
	call := &Call{
		ID:        id,
		Direction: "inbound",
		From:      strings.TrimSpace(from),
		To:        strings.TrimSpace(s.cfg.CallerID),
		Profile:   firstNonEmpty(s.cfg.SIPProfile, defaultSIPProfile),
		Status:    "incoming",
		CreatedAt: now,
		UpdatedAt: now,
		ChannelID: strings.TrimSpace(channel.ID),
		cancel:    cancel,
	}
	s.mu.Lock()
	s.calls[id] = call
	s.mu.Unlock()
	go s.runInboundCall(ctx, call)
}

func (s *Server) runInboundCall(ctx context.Context, call *Call) {
	defer call.cancel()
	if err := s.runInboundCallOnce(ctx, call); err != nil {
		s.updateCall(call, "failed", err.Error())
		log.Printf("telephony inbound call %s failed: %v", call.ID, err)
	}
}

func (s *Server) runInboundCallOnce(ctx context.Context, call *Call) error {
	channelID := strings.TrimSpace(call.ChannelID)
	if channelID == "" {
		return errors.New("inbound channel id is required")
	}
	req := createCallRequest{
		SystemInstruction: inboundSystemInstruction(s.cfg.InboundPrompt),
		InitialMessage:    firstNonEmpty(s.cfg.InboundGreeting, "Здравствуйте."),
		ExternalKey:       inboundExternalKey(call),
	}
	s.updateCall(call, "preparing", "")
	realtime, err := s.connectRealtime(ctx, call, req)
	if err != nil {
		return err
	}
	connected := false
	defer func() {
		if !connected {
			_ = realtime.Close(context.Background())
		}
	}()

	s.updateCall(call, "answering", "")
	if err := s.ari.answer(ctx, channelID); err != nil {
		return err
	}
	now := time.Now().UTC()
	call.AnsweredAt = &now
	s.updateCall(call, "answered", "")
	logCallTimeline(call, "", "answered", "direction", call.Direction, "channel", channelID)

	connected = true
	return s.runConnectedCallWithRealtime(ctx, call, req, channelID, realtime)
}

func (s *Server) runCallOnce(ctx context.Context, call *Call, req createCallRequest) error {
	callID := safeARIID(call.ID + "-call")
	call.ChannelID = callID

	s.updateCall(call, "preparing", "")

	events, unsubscribe := s.events.Subscribe(128)
	defer unsubscribe()
	if err := s.events.WaitReady(ctx); err != nil {
		return fmt.Errorf("wait for ARI app: %w", err)
	}

	s.updateCall(call, "dialing", "")

	endpoint := fmt.Sprintf("PJSIP/%s@%s", call.To, firstNonEmpty(call.Profile, s.cfg.SIPProfile))
	if err := s.ari.originate(ctx, originateRequest{
		ChannelID: callID,
		Endpoint:  endpoint,
		App:       s.cfg.ARIApp,
		AppArgs:   call.ID,
		CallerID:  s.cfg.CallerID,
		Timeout:   int(s.cfg.CallTimeout / time.Second),
		Formats:   "alaw",
	}); err != nil {
		return err
	}

	if _, err := waitForARIEvent(ctx, events, s.cfg.CallTimeout, func(event ariEvent) bool {
		return event.Type == "StasisStart" && event.Channel != nil && event.Channel.ID == callID
	}); err != nil {
		return fmt.Errorf("wait for call answer: %w", err)
	}
	if err := s.ari.answer(ctx, callID); err != nil {
		return err
	}
	now := time.Now().UTC()
	call.AnsweredAt = &now
	s.updateCall(call, "answered", "")
	logCallTimeline(call, "", "answered", "direction", call.Direction, "channel", callID)

	realtime, err := s.connectRealtime(ctx, call, req)
	if err != nil {
		_ = s.ari.hangup(context.Background(), callID)
		return err
	}
	return s.runConnectedCallWithRealtime(ctx, call, req, callID, realtime)
}

func (s *Server) connectRealtime(ctx context.Context, call *Call, req createCallRequest) (*realtimeConn, error) {
	realtimeClient := newRealtimeClient(s.cfg.MatrixclawURL, s.cfg.MatrixclawToken)
	realtime, err := realtimeClient.Connect(ctx, realtimeConnectRequest{
		Client:      "telephony",
		ExternalKey: firstNonEmpty(req.ExternalKey, call.ID),
		SessionID:   strings.TrimSpace(req.SessionID),
		SystemInstruction: phoneSystemInstruction(phonePromptInput{
			AssistantName:      firstNonEmpty(req.AssistantName, s.cfg.AssistantName),
			CallID:             call.ID,
			OpeningPhrase:      req.InitialMessage,
			PhonePrompt:        firstNonEmpty(req.PhonePrompt, s.cfg.PhonePrompt),
			CustomInstructions: req.AssistantCustomInstructions,
			Objective:          firstNonEmpty(req.SystemInstruction, req.Objective),
			Direction:          call.Direction,
		}),
	})
	if err != nil {
		return nil, err
	}
	call.RealtimeSessionID = realtime.Session.ID
	call.CoreSessionID = realtime.Session.CoreSessionID
	s.touchCall(call)
	return realtime, nil
}

func (s *Server) runConnectedCallWithRealtime(ctx context.Context, call *Call, req createCallRequest, channelID string, realtime *realtimeConn) error {
	if realtime == nil {
		return errors.New("realtime connection is required")
	}
	defer func() { _ = realtime.Close(context.Background()) }()

	captureRTP, playbackRTP, err := newRTPSessionPair(s.cfg.RTPBind, s.cfg.RTPExternalAddress)
	if err != nil {
		return err
	}
	defer captureRTP.Close()
	defer playbackRTP.Close()
	call.rtpIn = captureRTP
	call.rtpOut = playbackRTP
	captureRTP.SetDiagnostics(call.ID, realtime.Session.ID, "capture")
	playbackRTP.SetDiagnostics(call.ID, realtime.Session.ID, "playback")
	defer func() {
		s.syncCallStats(call)
		call.rtpIn = nil
		call.rtpOut = nil
	}()

	captureBridgeID := safeARIID(call.ID + "-capture-bridge")
	playbackBridgeID := safeARIID(call.ID + "-playback-bridge")
	captureSnoopID := safeARIID(call.ID + "-capture-snoop")
	captureExternalID := safeARIID(call.ID + "-capture-media")
	playbackExternalID := safeARIID(call.ID + "-playback-media")
	call.ChannelID = channelID
	call.BridgeID = playbackBridgeID
	call.ExternalChannelID = playbackExternalID
	call.CaptureBridgeID = captureBridgeID
	call.PlaybackBridgeID = playbackBridgeID
	call.CaptureSnoopChannelID = captureSnoopID
	call.PlaybackSnoopChannelID = ""
	call.CaptureExternalChannelID = captureExternalID
	call.PlaybackExternalChannelID = playbackExternalID

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			s.cleanupSplitARI(
				context.Background(),
				channelID,
				[]string{captureExternalID, playbackExternalID, captureSnoopID},
				[]string{captureBridgeID, playbackBridgeID},
			)
		})
	}
	defer cleanup()

	if err := s.ari.createBridge(ctx, captureBridgeID); err != nil {
		return err
	}
	if err := s.ari.createBridge(ctx, playbackBridgeID); err != nil {
		return err
	}
	if _, err := s.ari.snoop(ctx, snoopRequest{
		ChannelID: channelID,
		SnoopID:   captureSnoopID,
		App:       s.cfg.ARIApp,
		AppArgs:   call.ID + ",capture",
		Spy:       "in",
		Whisper:   "none",
	}); err != nil {
		return err
	}
	if _, err := s.ari.externalMedia(ctx, externalMediaRequest{
		ChannelID:    captureExternalID,
		App:          s.cfg.ARIApp,
		ExternalHost: captureRTP.ExternalHost(),
		Format:       "alaw",
		Data:         call.ID + ",capture",
	}); err != nil {
		return err
	}
	if _, err := s.ari.externalMedia(ctx, externalMediaRequest{
		ChannelID:    playbackExternalID,
		App:          s.cfg.ARIApp,
		ExternalHost: playbackRTP.ExternalHost(),
		Format:       "alaw",
		Data:         call.ID + ",playback",
	}); err != nil {
		return err
	}
	if err := s.ari.addChannelWithRetry(ctx, captureBridgeID, captureSnoopID); err != nil {
		return err
	}
	if err := s.ari.addChannelWithRetry(ctx, captureBridgeID, captureExternalID); err != nil {
		return err
	}
	if err := s.ari.addChannelWithRetry(ctx, playbackBridgeID, channelID); err != nil {
		return err
	}
	if err := s.ari.addChannelWithRetry(ctx, playbackBridgeID, playbackExternalID); err != nil {
		return err
	}
	if err := s.setRTPRemoteWithRetry(ctx, call, "capture", captureExternalID, captureRTP, false); err != nil {
		return err
	}
	if err := s.setRTPRemoteWithRetry(ctx, call, "playback", playbackExternalID, playbackRTP, true); err != nil {
		return err
	}

	recording := s.startChannelRecording(ctx, call, channelID)
	var finishRecordingOnce sync.Once
	finishRecording := func() {
		finishRecordingOnce.Do(func() {
			s.finishCallRecording(context.Background(), call, recording)
		})
	}
	defer finishRecording()

	audioErr := make(chan callRuntimeResult, 3)
	go func() {
		audioErr <- callRuntimeResult{source: "rtp_input", err: rtpToRealtime(ctx, captureRTP, realtime, call)}
	}()
	go func() {
		audioErr <- callRuntimeResult{source: "realtime_output", err: realtimeToRTP(ctx, realtime, playbackRTP, call)}
	}()
	go func() {
		audioErr <- callRuntimeResult{source: "ari_channel", err: s.ari.waitChannelEnd(ctx, channelID)}
	}()

	s.updateCall(call, "bridged", "")
	logCallTimeline(call, realtime.Session.ID, "bridged",
		"channel", channelID,
		"capture_bridge", captureBridgeID,
		"capture_channels", captureSnoopID+"+"+captureExternalID,
		"playback_bridge", playbackBridgeID,
		"playback_channels", channelID+"+"+playbackExternalID,
	)
	if prompt := initialPhoneStartPrompt(call, req); prompt != "" {
		log.Printf("telephony sending initial realtime prompt call=%s session=%s direction=%s", call.ID, realtime.Session.ID, call.Direction)
		if err := realtime.SendText(ctx, prompt); err != nil {
			log.Printf("telephony initial realtime prompt failed call=%s session=%s: %v", call.ID, realtime.Session.ID, err)
		} else {
			logCallTimeline(call, realtime.Session.ID, "initial_prompt_sent", "direction", call.Direction, "bytes", len(prompt))
		}
	}

	select {
	case <-ctx.Done():
		logCallTimeline(call, realtime.Session.ID, "runtime_end", "source", "context", "error", ctx.Err())
	case result := <-audioErr:
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			log.Printf("telephony call runtime failed call=%s source=%s: %v", call.ID, result.source, result.err)
			logCallTimeline(call, realtime.Session.ID, "runtime_end", "source", result.source, "error", result.err)
			finishRecording()
			cleanup()
			return result.err
		}
		log.Printf("telephony call runtime ended call=%s source=%s", call.ID, result.source)
		logCallTimeline(call, realtime.Session.ID, "runtime_end", "source", result.source)
	}
	finishRecording()
	cleanup()
	s.updateCall(call, "finished", "")
	return nil
}

func (s *Server) setRTPRemoteWithRetry(ctx context.Context, call *Call, label string, channelID string, rtp *rtpSession, required bool) error {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		addr, err := s.ari.rtpAddress(ctx, channelID)
		if err == nil {
			rtp.SetRemote(addr)
			callID := ""
			if call != nil {
				callID = call.ID
			}
			log.Printf("telephony RTP remote set call=%s label=%s remote=%s channel=%s", callID, label, addr.String(), channelID)
			logCallTimeline(call, "", "rtp_remote_"+strings.TrimSpace(label), "channel", channelID, "remote", addr.String())
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !sleepContext(ctx, time.Duration(100+attempt*75)*time.Millisecond) {
			return ctx.Err()
		}
	}
	callID := ""
	if call != nil {
		callID = call.ID
	}
	if required {
		return fmt.Errorf("telephony RTP remote not available call=%s label=%s channel=%s: %w", callID, label, channelID, lastErr)
	}
	log.Printf("telephony RTP remote unavailable call=%s label=%s channel=%s: %v", callID, label, channelID, lastErr)
	return nil
}

type callRuntimeResult struct {
	source string
	err    error
}

func initialPhoneStartPrompt(call *Call, req createCallRequest) string {
	phrase := strings.TrimSpace(req.InitialMessage)
	if phrase == "" {
		phrase = "Здравствуйте."
	}
	direction := ""
	if call != nil {
		direction = strings.TrimSpace(call.Direction)
	}
	objective := strings.TrimSpace(firstNonEmpty(req.SystemInstruction, req.Objective))
	parts := []string{
		"The phone call is connected. Begin speaking now.",
		"Say a short natural opening using this phrase: " + phrase,
	}
	switch direction {
	case "inbound":
		parts = append(parts, "Then ask one short question about how you can help and wait.")
	case "outbound":
		if objective != "" {
			parts = append(parts, "Then briefly state the practical reason for the call based on the call objective and wait.")
		} else {
			parts = append(parts, "Then wait for the other person to answer.")
		}
	default:
		parts = append(parts, "Then wait for the other person to answer.")
	}
	if objective != "" {
		parts = append(parts, "Call objective: "+objective)
	}
	return strings.Join(parts, "\n")
}

func (s *Server) cleanupSplitARI(ctx context.Context, channelID string, extraChannels []string, bridgeIDs []string) {
	_ = s.ari.stopSilence(ctx, channelID)
	_ = s.ari.hangup(ctx, channelID)
	for _, id := range extraChannels {
		_ = s.ari.hangup(ctx, id)
	}
	for _, id := range bridgeIDs {
		_ = s.ari.destroyBridge(ctx, id)
	}
}

func (s *Server) cleanupStaleARIOnReady(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	if err := s.events.WaitReady(ctx); err != nil {
		if parent.Err() == nil {
			log.Printf("telephony stale ARI cleanup skipped: %v", err)
		}
		return
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(parent, 15*time.Second)
	defer cleanupCancel()
	if err := s.cleanupStaleARI(cleanupCtx); err != nil && cleanupCtx.Err() == nil {
		log.Printf("telephony stale ARI cleanup failed: %v", err)
	}
}

func (s *Server) cleanupStaleARI(ctx context.Context) error {
	if s == nil || s.ari == nil {
		return nil
	}
	channels, err := s.ari.channels(ctx)
	if err != nil {
		return err
	}
	removedChannels := 0
	for _, channel := range channels {
		if !s.ownsARIChannel(channel) {
			continue
		}
		if s.activeARIChannel(channel.ID) {
			continue
		}
		if err := s.ari.hangup(ctx, channel.ID); err != nil {
			log.Printf("telephony stale ARI channel cleanup failed channel=%s: %v", strings.TrimSpace(channel.ID), err)
			continue
		}
		removedChannels++
	}

	bridges, err := s.ari.bridges(ctx)
	if err != nil {
		return err
	}
	removedBridges := 0
	for _, bridge := range bridges {
		if !s.ownsARIBridge(bridge) {
			continue
		}
		if s.activeARIBridge(bridge.ID) {
			continue
		}
		if err := s.ari.destroyBridge(ctx, bridge.ID); err != nil {
			log.Printf("telephony stale ARI bridge cleanup failed bridge=%s: %v", strings.TrimSpace(bridge.ID), err)
			continue
		}
		removedBridges++
	}
	if removedChannels > 0 || removedBridges > 0 {
		log.Printf("telephony stale ARI cleanup removed channels=%d bridges=%d", removedChannels, removedBridges)
	}
	return nil
}

func (s *Server) ownsARIChannel(channel ariChannel) bool {
	app := strings.TrimSpace(s.cfg.ARIApp)
	id := strings.TrimSpace(channel.ID)
	name := strings.TrimSpace(channel.Name)
	appName := strings.TrimSpace(channel.Dialplan.AppName)
	appData := strings.TrimSpace(channel.Dialplan.AppData)
	if app != "" && strings.EqualFold(appName, "Stasis") && (appData == app || strings.HasPrefix(appData, app+",")) {
		return true
	}
	ownedID := strings.HasPrefix(id, "call_") && (strings.HasSuffix(id, "-call") ||
		strings.HasSuffix(id, "-media") ||
		strings.HasSuffix(id, "-capture-media") ||
		strings.HasSuffix(id, "-playback-media") ||
		strings.HasSuffix(id, "-capture-snoop") ||
		strings.HasSuffix(id, "-playback-snoop"))
	ownedExternalMedia := strings.HasPrefix(name, "UnicastRTP/") && app != "" && strings.Contains(appData, app)
	return ownedID || ownedExternalMedia
}

func (s *Server) ownsARIBridge(bridge ariBridge) bool {
	id := strings.TrimSpace(bridge.ID)
	name := strings.TrimSpace(bridge.Name)
	ownedID := strings.HasPrefix(id, "call_") && strings.HasSuffix(id, "-bridge")
	ownedName := strings.HasPrefix(name, "call_") && strings.HasSuffix(name, "-bridge")
	return ownedID || ownedName
}

func (s *Server) activeARIChannel(channelID string) bool {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, call := range s.calls {
		if call == nil {
			continue
		}
		if channelID == strings.TrimSpace(call.ChannelID) ||
			channelID == strings.TrimSpace(call.ExternalChannelID) ||
			channelID == strings.TrimSpace(call.CaptureExternalChannelID) ||
			channelID == strings.TrimSpace(call.PlaybackExternalChannelID) ||
			channelID == strings.TrimSpace(call.CaptureSnoopChannelID) ||
			channelID == strings.TrimSpace(call.PlaybackSnoopChannelID) {
			return true
		}
	}
	return false
}

func (s *Server) activeARIBridge(bridgeID string) bool {
	bridgeID = strings.TrimSpace(bridgeID)
	if bridgeID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, call := range s.calls {
		if call != nil && (bridgeID == strings.TrimSpace(call.BridgeID) ||
			bridgeID == strings.TrimSpace(call.CaptureBridgeID) ||
			bridgeID == strings.TrimSpace(call.PlaybackBridgeID)) {
			return true
		}
	}
	return false
}

func (s *Server) postCallReport(parent context.Context, call *Call, req createCallRequest) {
	if !shouldPostCallReport(call, req) {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	text := postCallReportPrompt(call, req)
	if strings.TrimSpace(text) == "" {
		return
	}
	payload := map[string]any{
		"client":              firstNonEmpty(req.OriginClient, call.OriginClient),
		"external_key":        firstNonEmpty(req.OriginExternalKey, call.OriginExternalKey),
		"session_id":          firstNonEmpty(req.OriginSessionID, call.OriginSessionID, call.CoreSessionID),
		"text":                text,
		"busy_mode":           "queue",
		"allow_auto_bind_one": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("telephony call %s post-call report encode failed: %v", callID(call), err)
		return
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.MatrixclawURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		log.Printf("telephony call %s post-call report request failed: %v", callID(call), err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.cfg.MatrixclawToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.MatrixclawToken)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(httpReq)
	if err != nil {
		log.Printf("telephony call %s post-call report delivery failed: %v", callID(call), err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("telephony call %s post-call report returned HTTP %d", callID(call), res.StatusCode)
		return
	}
	log.Printf("telephony call %s post-call report queued", callID(call))
}

func shouldPostCallReport(call *Call, req createCallRequest) bool {
	if call == nil || !strings.EqualFold(strings.TrimSpace(call.Direction), "outbound") {
		return false
	}
	if req.PostCallReport != nil && !*req.PostCallReport {
		return false
	}
	if firstNonEmpty(req.OriginClient, call.OriginClient) == "" {
		return false
	}
	if firstNonEmpty(req.OriginExternalKey, call.OriginExternalKey) == "" {
		return false
	}
	return true
}

func postCallReportPrompt(call *Call, req createCallRequest) string {
	if call == nil {
		return ""
	}
	turns := callTranscriptSnapshot(call)
	var sections []string
	sections = append(sections, strings.TrimSpace(`Phone call completed. Write a concise report to the user in the user's language. Do not place another phone call. Explain whether the phone objective succeeded, list the important facts, mention any errors or unanswered items, and include a readable transcript of the conversation.`))
	sections = append(sections, "Call status:\n"+formatCallStatus(call))
	if objective := firstNonEmpty(call.Objective, req.Objective, req.SystemInstruction); objective != "" {
		sections = append(sections, "Original phone objective:\n"+objective)
	}
	if transcript := formatTranscript(turns); transcript != "" {
		sections = append(sections, "Transcript:\n"+transcript)
	} else {
		sections = append(sections, "Transcript:\nNo transcript was captured.")
	}
	if recording := callRecordingSnapshot(call); recording != nil {
		sections = append(sections, "Recording:\n"+formatCallRecording(*recording))
	}
	return strings.Join(sections, "\n\n")
}

func formatCallStatus(call *Call) string {
	var lines []string
	lines = append(lines, "id: "+strings.TrimSpace(call.ID))
	lines = append(lines, "status: "+strings.TrimSpace(call.Status))
	if call.To != "" {
		lines = append(lines, "to: "+strings.TrimSpace(call.To))
	}
	if call.From != "" {
		lines = append(lines, "from: "+strings.TrimSpace(call.From))
	}
	if call.AnsweredAt != nil {
		lines = append(lines, "answered_at: "+call.AnsweredAt.Format(time.RFC3339))
	}
	if call.FinishedAt != nil {
		lines = append(lines, "finished_at: "+call.FinishedAt.Format(time.RFC3339))
	}
	if call.Error != "" {
		lines = append(lines, "error: "+strings.TrimSpace(call.Error))
	}
	return strings.Join(lines, "\n")
}

func callSnapshot(call *Call) CallSnapshot {
	if call == nil {
		return CallSnapshot{}
	}
	call.transcriptMu.Lock()
	inputTranscript := call.InputTranscript
	assistantTranscript := call.AssistantTranscript
	transcript := make([]CallTranscriptTurn, len(call.Transcript))
	copy(transcript, call.Transcript)
	call.transcriptMu.Unlock()

	return CallSnapshot{
		ID:                        call.ID,
		Direction:                 call.Direction,
		To:                        call.To,
		From:                      call.From,
		Profile:                   call.Profile,
		Objective:                 call.Objective,
		Status:                    call.Status,
		Error:                     call.Error,
		RealtimeSessionID:         call.RealtimeSessionID,
		CoreSessionID:             call.CoreSessionID,
		OriginClient:              call.OriginClient,
		OriginExternalKey:         call.OriginExternalKey,
		OriginSessionID:           call.OriginSessionID,
		BridgeID:                  call.BridgeID,
		ChannelID:                 call.ChannelID,
		ExternalChannelID:         call.ExternalChannelID,
		CaptureBridgeID:           call.CaptureBridgeID,
		PlaybackBridgeID:          call.PlaybackBridgeID,
		CaptureSnoopChannelID:     call.CaptureSnoopChannelID,
		PlaybackSnoopChannelID:    call.PlaybackSnoopChannelID,
		CaptureExternalChannelID:  call.CaptureExternalChannelID,
		PlaybackExternalChannelID: call.PlaybackExternalChannelID,
		CreatedAt:                 call.CreatedAt,
		UpdatedAt:                 call.UpdatedAt,
		AnsweredAt:                call.AnsweredAt,
		FinishedAt:                call.FinishedAt,
		InputTranscript:           inputTranscript,
		AssistantTranscript:       assistantTranscript,
		Transcript:                transcript,
		Recording:                 callRecordingSnapshot(call),
		RTP:                       call.RTP,
		RTPCapture:                call.RTPCapture,
		RTPPlayback:               call.RTPPlayback,
	}
}

func callTranscriptSnapshot(call *Call) []CallTranscriptTurn {
	if call == nil {
		return nil
	}
	call.transcriptMu.Lock()
	defer call.transcriptMu.Unlock()
	if len(call.Transcript) == 0 {
		var fallback []CallTranscriptTurn
		if text := strings.TrimSpace(call.InputTranscript); text != "" {
			fallback = append(fallback, CallTranscriptTurn{
				Speaker: "caller",
				Text:    text,
			})
		}
		if text := strings.TrimSpace(call.AssistantTranscript); text != "" {
			fallback = append(fallback, CallTranscriptTurn{
				Speaker: "assistant",
				Text:    text,
			})
		}
		return fallback
	}
	out := make([]CallTranscriptTurn, len(call.Transcript))
	copy(out, call.Transcript)
	return out
}

func formatTranscript(turns []CallTranscriptTurn) string {
	var lines []string
	for _, turn := range turns {
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}
		speaker := "Caller"
		if strings.EqualFold(turn.Speaker, "assistant") {
			speaker = "Assistant"
		}
		lines = append(lines, speaker+": "+text)
	}
	if len(lines) == 0 {
		return ""
	}
	return clipText(strings.Join(lines, "\n"), 20000)
}

func clipText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "\n[transcript truncated]"
}

func callID(call *Call) string {
	if call == nil {
		return ""
	}
	return strings.TrimSpace(call.ID)
}

func (s *Server) updateCall(call *Call, status string, errText string) {
	call.Status = status
	call.Error = strings.TrimSpace(errText)
	call.UpdatedAt = time.Now().UTC()
	if status == "failed" || status == "finished" {
		now := call.UpdatedAt
		call.FinishedAt = &now
	}
}

func (s *Server) touchCall(call *Call) {
	s.syncCallStats(call)
	call.UpdatedAt = time.Now().UTC()
}

func (s *Server) syncCallStats(call *Call) {
	if call == nil {
		return
	}
	if call.rtp != nil {
		call.RTP = call.rtp.Stats()
		call.RTPCapture = call.RTP
		return
	}
	if call.rtpIn != nil || call.rtpOut != nil {
		call.RTP = combineRTPStats(call.rtpIn, call.rtpOut)
		if call.rtpIn != nil {
			call.RTPCapture = call.rtpIn.Stats()
		}
		if call.rtpOut != nil {
			call.RTPPlayback = call.rtpOut.Stats()
		}
	}
}

func combineRTPStats(in *rtpSession, out *rtpSession) rtpStats {
	stats := rtpStats{}
	inRemote := ""
	if in != nil {
		stats = in.Stats()
		inRemote = stats.Remote
	}
	outRemote := ""
	if out != nil {
		outStats := out.Stats()
		stats.OutPackets = outStats.OutPackets
		stats.OutBytes = outStats.OutBytes
		outRemote = outStats.Remote
		if in == nil {
			stats.InPackets = outStats.InPackets
			stats.InBytes = outStats.InBytes
			stats.InAvgAbs = outStats.InAvgAbs
			stats.InAvg = outStats.InAvg
			stats.InPeak = outStats.InPeak
			stats.SpeechFrames = outStats.SpeechFrames
		}
	}
	switch {
	case inRemote != "" && outRemote != "":
		stats.Remote = "in=" + inRemote + " out=" + outRemote
	case inRemote != "":
		stats.Remote = "in=" + inRemote
	case outRemote != "":
		stats.Remote = "out=" + outRemote
	}
	return stats
}

func (s *Server) call(id string) (*Call, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	call, ok := s.calls[strings.TrimSpace(id)]
	return call, ok
}

func (s *Server) probeMatrixclaw(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.MatrixclawURL+"/v1/modules/voice/realtime_voice", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.MatrixclawToken)
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var payload struct {
		Module struct {
			Enabled bool   `json:"enabled"`
			Status  string `json:"status"`
		} `json:"module"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return err
	}
	if !payload.Module.Enabled {
		return errors.New("realtime voice disabled")
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Module.Status), "Ready") {
		return fmt.Errorf("realtime voice status is %s", payload.Module.Status)
	}
	return nil
}

type phonePromptInput struct {
	AssistantName      string
	CallID             string
	OpeningPhrase      string
	PhonePrompt        string
	CustomInstructions string
	Objective          string
	Direction          string
}

func phoneSystemInstruction(input phonePromptInput) string {
	sections := []string{strings.TrimSpace(`You are speaking on a live phone call through MatrixClaw telephony.

Identity:
- Your configured assistant name is provided below. If asked who you are, use exactly that name and do not invent another identity.
- If a user, owner, or client name is provided in the call objective, phone instructions, or user custom instructions, you may identify yourself as that person's assistant. Never invent this person name.
- Do not mention internal systems, realtime audio, Gemini, Grok, Asterisk, SIP, RTP, call IDs, tools, or MatrixClaw unless the human explicitly asks about the technical system.

Opening behavior:
- If the gateway sends an explicit instruction that the phone call is connected and asks you to begin speaking, follow that instruction immediately with one short natural opening, then wait.
- Otherwise, do not start speaking just because the phone call was answered. Stay silent until the other side says the first meaningful human words, such as a greeting or a clear question.
- If the first sound is silence, ringing tone, queue music, beeps, voicemail, an automated IVR, or a call center robot, do not introduce yourself over it. Wait for a human when possible.
- If an automated menu asks for a simple input that is necessary for the call objective, answer only that menu step briefly, then keep waiting for a human.
- After the first meaningful human utterance, greet once and introduce yourself naturally using the configured assistant name. If an owner/client name is known from context, a Russian introduction may sound like: "Здравствуйте, я ассистент <owner>, меня зовут <assistant name>." If no owner/client name is known, do not invent one.
- If the call is inbound, after the caller speaks first, greet briefly and ask how you can help within the configured phone objective.
- Do not repeat the full introduction later unless the human asks who you are.

Conversation style:
- Speak like a calm human on the phone, not like a chatbot or a scripted IVR.
- Use short, natural spoken phrases. Usually answer in one or two sentences, then stop.
- Do not over-explain, list options, narrate your reasoning, or repeat the user's words unless confirming an important detail.
- If you need information, ask one clear question and wait.
- If you did not understand speech, say so briefly and ask the person to repeat it naturally.
- Do not repeatedly ask the person to speak, continue, or say something. If you have already greeted them or asked one question, stop speaking and wait.
- Do not invent personal facts. If the caller asks for a name, number, address, time, booking, or other fact that is not known from the conversation/context, say that you do not know yet and ask for the missing detail.
- For outbound calls, introduce yourself briefly, state the practical reason for the call, and move directly toward the objective.
- Confirm critical details such as names, phone numbers, dates, times, addresses, prices, and bookings before treating them as final.
- Keep speaking the established phone conversation language. Do not switch to English because of uncertain speech recognition unless the human explicitly asks to use English.
- When speaking Russian, use natural conversational Russian and avoid English or Spanish filler words.
- Do not flirt, joke at length, or continue casual off-topic chat unless it directly helps complete the call objective.

Scope and privacy:
- Stay within the call objective and the practical details needed to complete it.
- Do not provide unrelated facts, advice, explanations, personal data, credentials, internal project information, or anything not necessary for the call objective.
- If the human asks an unrelated question, briefly redirect once to the call objective.
- If the human repeatedly tries to move the call to unrelated topics after a redirect, say a short goodbye and end the call.
- If the objective is complete, impossible, refused, or the other person clearly has nothing else relevant to add, summarize the practical result in one short sentence, say goodbye, and end the call.

Ending the call:
- To end the call, first say one short natural goodbye phrase, then call the telephony_end_call tool with the current call_id.
- Do not say that you are calling a tool. Do not read out or mention the call_id.
- Do not use telephony_call during an active phone conversation.`)}
	if name := strings.TrimSpace(input.AssistantName); name != "" {
		sections = append(sections, "Assistant name:\n"+name)
	}
	if callID := strings.TrimSpace(input.CallID); callID != "" {
		sections = append(sections, "Current call_id for telephony_end_call:\n"+callID)
	}
	if direction := strings.TrimSpace(input.Direction); direction != "" {
		sections = append(sections, "Call direction:\n"+direction)
	}
	if opening := strings.TrimSpace(input.OpeningPhrase); opening != "" {
		sections = append(sections, "Preferred first assistant phrase after the first meaningful human utterance:\n"+opening)
	}
	if phonePrompt := strings.TrimSpace(input.PhonePrompt); phonePrompt != "" {
		sections = append(sections, "Phone assistant instructions:\n"+phonePrompt)
	}
	if customInstructions := strings.TrimSpace(input.CustomInstructions); customInstructions != "" {
		sections = append(sections, "User custom instructions:\n"+customInstructions)
	}
	if objective := strings.TrimSpace(input.Objective); objective != "" {
		sections = append(sections, "Call objective:\n"+objective)
	} else {
		sections = append(sections, "Call objective:\nHandle the caller's immediate phone request. Do not expand beyond what the caller asks for in this phone call.")
	}
	return strings.Join(sections, "\n\n")
}

func inboundSystemInstruction(custom string) string {
	base := strings.TrimSpace(`You are answering an inbound phone call. If the gateway sends an explicit instruction that the call is connected and asks you to begin speaking, follow it once with a short natural greeting. Otherwise, wait for the caller's first meaningful words, then greet briefly and handle the caller's request within the configured phone objective. Do not repeatedly say that you are listening or ask the caller to continue. Speak in short natural phone phrases, not as a scripted chatbot. If the caller asks who you are, use the configured assistant identity. If you do not know a personal fact, do not guess; ask for it. Keep answers short and wait after each question.`)
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return base
	}
	return base + "\n\nInbound call behavior:\n" + custom
}

func inboundExternalKey(call *Call) string {
	if call != nil && strings.TrimSpace(call.From) != "" {
		return "inbound:" + strings.TrimSpace(call.From)
	}
	if call != nil {
		return call.ID
	}
	return ""
}

func ariChannelSummary(channel *ariChannel) string {
	if channel == nil {
		return "channel=<nil>"
	}
	var parts []string
	if strings.TrimSpace(channel.ID) != "" {
		parts = append(parts, "id="+strings.TrimSpace(channel.ID))
	}
	if strings.TrimSpace(channel.Name) != "" {
		parts = append(parts, "name="+strings.TrimSpace(channel.Name))
	}
	if strings.TrimSpace(channel.Caller.Number) != "" || strings.TrimSpace(channel.Caller.Name) != "" {
		parts = append(parts, "caller="+strings.TrimSpace(channel.Caller.Name)+"<"+strings.TrimSpace(channel.Caller.Number)+">")
	}
	if strings.TrimSpace(channel.Connected.Number) != "" || strings.TrimSpace(channel.Connected.Name) != "" {
		parts = append(parts, "connected="+strings.TrimSpace(channel.Connected.Name)+"<"+strings.TrimSpace(channel.Connected.Number)+">")
	}
	if strings.TrimSpace(channel.Dialplan.Context) != "" || strings.TrimSpace(channel.Dialplan.Exten) != "" {
		parts = append(parts, "dialplan="+strings.TrimSpace(channel.Dialplan.Context)+"/"+strings.TrimSpace(channel.Dialplan.Exten))
	}
	if len(parts) == 0 {
		return "channel=unknown"
	}
	return strings.Join(parts, " ")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func normalizePhone(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "+")
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	phone := b.String()
	if len(phone) == 11 && phone[0] == '8' {
		return "7" + phone[1:]
	}
	return phone
}

func safeARIID(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
