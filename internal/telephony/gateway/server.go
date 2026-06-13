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
	rtp                        *rtpSession
	currentInputTranscript     string
	currentAssistantTranscript string
}

type CallSnapshot struct {
	ID                  string               `json:"id"`
	Direction           string               `json:"direction,omitempty"`
	To                  string               `json:"to"`
	From                string               `json:"from,omitempty"`
	Profile             string               `json:"profile,omitempty"`
	Objective           string               `json:"objective,omitempty"`
	Status              string               `json:"status"`
	Error               string               `json:"error,omitempty"`
	RealtimeSessionID   string               `json:"realtime_session_id,omitempty"`
	CoreSessionID       string               `json:"session_id,omitempty"`
	OriginClient        string               `json:"origin_client,omitempty"`
	OriginExternalKey   string               `json:"origin_external_key,omitempty"`
	OriginSessionID     string               `json:"origin_session_id,omitempty"`
	BridgeID            string               `json:"bridge_id,omitempty"`
	ChannelID           string               `json:"channel_id,omitempty"`
	ExternalChannelID   string               `json:"external_channel_id,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	AnsweredAt          *time.Time           `json:"answered_at,omitempty"`
	FinishedAt          *time.Time           `json:"finished_at,omitempty"`
	InputTranscript     string               `json:"input_transcript,omitempty"`
	AssistantTranscript string               `json:"assistant_transcript,omitempty"`
	Transcript          []CallTranscriptTurn `json:"transcript,omitempty"`
	Recording           *CallRecording       `json:"recording,omitempty"`
	RTP                 rtpStats             `json:"rtp,omitempty"`
}

type createCallRequest struct {
	To                          string `json:"to"`
	Profile                     string `json:"profile,omitempty"`
	Objective                   string `json:"objective,omitempty"`
	SystemInstruction           string `json:"system_instruction,omitempty"`
	InitialMessage              string `json:"initial_message,omitempty"`
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
	return &Server{
		cfg:    cfg,
		ari:    newARIClient(cfg.ARIURL, cfg.ARIUser, cfg.ARIPassword),
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
	for {
		if ctx.Err() != nil {
			return
		}
		if s.cfg.ARIPassword == "" || s.cfg.MatrixclawToken == "" {
			log.Printf("telephony inbound listener disabled until ARI and MatrixClaw credentials are configured")
			select {
			case <-time.After(10 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		events, err := s.ari.events(ctx, s.cfg.ARIApp)
		if err != nil {
			log.Printf("telephony inbound listener connect failed: %v", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		log.Printf("telephony inbound listener registered ARI app %s", s.cfg.ARIApp)
		for {
			event, err := events.read(ctx)
			if err != nil {
				events.Close()
				if ctx.Err() != nil {
					return
				}
				log.Printf("telephony inbound listener disconnected: %v", err)
				break
			}
			if s.isInboundStart(event) {
				s.startInboundCall(ctx, event)
			}
		}
		select {
		case <-time.After(2 * time.Second):
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
	s.updateCall(call, "answering", "")
	if err := s.ari.answer(ctx, channelID); err != nil {
		return err
	}
	now := time.Now().UTC()
	call.AnsweredAt = &now
	s.updateCall(call, "answered", "")

	req := createCallRequest{
		SystemInstruction: inboundSystemInstruction(s.cfg.InboundPrompt),
		InitialMessage:    firstNonEmpty(s.cfg.InboundGreeting, "Здравствуйте. Я слушаю вас."),
		ExternalKey:       inboundExternalKey(call),
	}
	return s.runConnectedCall(ctx, call, req, channelID)
}

func (s *Server) runCallOnce(ctx context.Context, call *Call, req createCallRequest) error {
	events, err := s.ari.events(ctx, s.cfg.ARIApp)
	if err != nil {
		return err
	}
	defer events.Close()

	callID := safeARIID(call.ID + "-call")
	bridgeID := safeARIID(call.ID + "-bridge")
	externalID := safeARIID(call.ID + "-media")
	call.ChannelID = callID
	call.BridgeID = bridgeID
	call.ExternalChannelID = externalID
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

	if err := events.waitFor(ctx, s.cfg.CallTimeout, func(event ariEvent) bool {
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

	return s.runConnectedCall(ctx, call, req, callID)
}

func (s *Server) runConnectedCall(ctx context.Context, call *Call, req createCallRequest, channelID string) error {
	rtp, err := newRTPSession(s.cfg.RTPBind, s.cfg.RTPExternalAddress)
	if err != nil {
		return err
	}
	defer rtp.Close()
	call.rtp = rtp
	defer func() {
		s.syncCallStats(call)
		call.rtp = nil
	}()

	realtimeClient := newRealtimeClient(s.cfg.MatrixclawURL, s.cfg.MatrixclawToken)
	realtime, err := realtimeClient.Connect(ctx, realtimeConnectRequest{
		Client:            "telephony",
		ExternalKey:       firstNonEmpty(req.ExternalKey, call.ID),
		SessionID:         strings.TrimSpace(req.SessionID),
		SystemInstruction: phoneSystemInstruction(firstNonEmpty(req.PhonePrompt, s.cfg.PhonePrompt), req.AssistantCustomInstructions, firstNonEmpty(req.SystemInstruction, req.Objective)),
	})
	if err != nil {
		return err
	}
	defer realtime.Close(context.Background())
	call.RealtimeSessionID = realtime.Session.ID
	call.CoreSessionID = realtime.Session.CoreSessionID
	s.touchCall(call)

	bridgeID := firstNonEmpty(call.BridgeID, safeARIID(call.ID+"-bridge"))
	externalID := firstNonEmpty(call.ExternalChannelID, safeARIID(call.ID+"-media"))
	call.ChannelID = channelID
	call.BridgeID = bridgeID
	call.ExternalChannelID = externalID

	if err := s.ari.createBridge(ctx, bridgeID); err != nil {
		return err
	}
	if err := s.ari.addChannel(ctx, bridgeID, channelID); err != nil {
		return err
	}
	if _, err := s.ari.externalMedia(ctx, externalMediaRequest{
		ChannelID:    externalID,
		App:          s.cfg.ARIApp,
		ExternalHost: rtp.ExternalHost(),
		Format:       "alaw",
		Data:         call.ID,
	}); err != nil {
		return err
	}
	if err := s.ari.addChannel(ctx, bridgeID, externalID); err != nil {
		time.Sleep(250 * time.Millisecond)
		if retryErr := s.ari.addChannel(ctx, bridgeID, externalID); retryErr != nil {
			return retryErr
		}
	}
	if addr, err := s.ari.rtpAddress(ctx, externalID); err == nil {
		rtp.SetRemote(addr)
	}

	recording := s.startCallRecording(ctx, call, bridgeID)
	var finishRecordingOnce sync.Once
	finishRecording := func() {
		finishRecordingOnce.Do(func() {
			s.finishCallRecording(context.Background(), call, recording)
		})
	}
	defer finishRecording()

	audioErr := make(chan error, 3)
	gate := &mediaGate{}
	go func() { audioErr <- rtpToRealtime(ctx, rtp, realtime, gate) }()
	go func() { audioErr <- realtimeToRTP(ctx, realtime, rtp, call, gate) }()
	go func() { audioErr <- s.ari.waitChannelEnd(ctx, channelID) }()

	s.updateCall(call, "bridged", "")
	initial := strings.TrimSpace(req.InitialMessage)
	if initial == "" {
		initial = "Поздоровайся и начни телефонный разговор по задаче пользователя."
	}
	if err := realtime.SendText(ctx, initial); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
	case err := <-audioErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			finishRecording()
			s.cleanupARI(context.Background(), channelID, externalID, bridgeID)
			return err
		}
	}
	finishRecording()
	s.cleanupARI(context.Background(), channelID, externalID, bridgeID)
	s.updateCall(call, "finished", "")
	return nil
}

func (s *Server) cleanupARI(ctx context.Context, channelID string, externalID string, bridgeID string) {
	_ = s.ari.hangup(ctx, channelID)
	_ = s.ari.hangup(ctx, externalID)
	_ = s.ari.destroyBridge(ctx, bridgeID)
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
		ID:                  call.ID,
		Direction:           call.Direction,
		To:                  call.To,
		From:                call.From,
		Profile:             call.Profile,
		Objective:           call.Objective,
		Status:              call.Status,
		Error:               call.Error,
		RealtimeSessionID:   call.RealtimeSessionID,
		CoreSessionID:       call.CoreSessionID,
		OriginClient:        call.OriginClient,
		OriginExternalKey:   call.OriginExternalKey,
		OriginSessionID:     call.OriginSessionID,
		BridgeID:            call.BridgeID,
		ChannelID:           call.ChannelID,
		ExternalChannelID:   call.ExternalChannelID,
		CreatedAt:           call.CreatedAt,
		UpdatedAt:           call.UpdatedAt,
		AnsweredAt:          call.AnsweredAt,
		FinishedAt:          call.FinishedAt,
		InputTranscript:     inputTranscript,
		AssistantTranscript: assistantTranscript,
		Transcript:          transcript,
		Recording:           callRecordingSnapshot(call),
		RTP:                 call.RTP,
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
	if call != nil && call.rtp != nil {
		call.RTP = call.rtp.Stats()
	}
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

func phoneSystemInstruction(phonePrompt string, customInstructions string, objective string) string {
	sections := []string{strings.TrimSpace(`You are speaking on a live phone call through MatrixClaw telephony. Keep replies short, natural, and suitable for a real-time voice conversation. Do not mention internal systems, realtime audio, Gemini, Asterisk, SIP, RTP, or MatrixClaw unless the human asks. If you need information from the human, ask one short question and wait.`)}
	if phonePrompt = strings.TrimSpace(phonePrompt); phonePrompt != "" {
		sections = append(sections, "Phone assistant instructions:\n"+phonePrompt)
	}
	if customInstructions = strings.TrimSpace(customInstructions); customInstructions != "" {
		sections = append(sections, "User custom instructions:\n"+customInstructions)
	}
	if objective = strings.TrimSpace(objective); objective != "" {
		sections = append(sections, "Call objective:\n"+objective)
	}
	return strings.Join(sections, "\n\n")
}

func inboundSystemInstruction(custom string) string {
	base := strings.TrimSpace(`You are answering an inbound phone call. Greet the caller briefly, ask how you can help, and then follow the caller's request within the current phone conversation. If the caller asks who you are, say you are an AI phone assistant. Keep answers short and wait after each question.`)
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
