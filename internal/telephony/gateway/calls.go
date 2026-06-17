package gateway

import (
	"context"
	"strings"
	"sync"
	"time"
)

const finishedCallRetention = 24 * time.Hour

type Call struct {
	mu                         sync.Mutex
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

func callSnapshot(call *Call) CallSnapshot {
	if call == nil {
		return CallSnapshot{}
	}
	call.mu.Lock()
	defer call.mu.Unlock()

	transcript := make([]CallTranscriptTurn, len(call.Transcript))
	copy(transcript, call.Transcript)

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
		AnsweredAt:                cloneTimePtr(call.AnsweredAt),
		FinishedAt:                cloneTimePtr(call.FinishedAt),
		InputTranscript:           call.InputTranscript,
		AssistantTranscript:       call.AssistantTranscript,
		Transcript:                transcript,
		Recording:                 callRecordingSnapshotLocked(call),
		RTP:                       call.RTP,
		RTPCapture:                call.RTPCapture,
		RTPPlayback:               call.RTPPlayback,
	}
}

func callTranscriptSnapshot(call *Call) []CallTranscriptTurn {
	if call == nil {
		return nil
	}
	call.mu.Lock()
	defer call.mu.Unlock()
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
	call.mu.Lock()
	defer call.mu.Unlock()
	return strings.TrimSpace(call.ID)
}

func callDirection(call *Call) string {
	if call == nil {
		return ""
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	return strings.TrimSpace(call.Direction)
}

func callChannelID(call *Call) string {
	if call == nil {
		return ""
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	return strings.TrimSpace(call.ChannelID)
}

func callRealtimeSessionID(call *Call) string {
	if call == nil {
		return ""
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	return strings.TrimSpace(call.RealtimeSessionID)
}

func callARIChannelIDs(call *Call) []string {
	if call == nil {
		return nil
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	return []string{
		strings.TrimSpace(call.ChannelID),
		strings.TrimSpace(call.ExternalChannelID),
		strings.TrimSpace(call.CaptureExternalChannelID),
		strings.TrimSpace(call.PlaybackExternalChannelID),
		strings.TrimSpace(call.CaptureSnoopChannelID),
		strings.TrimSpace(call.PlaybackSnoopChannelID),
	}
}

func callARIBridgeIDs(call *Call) []string {
	if call == nil {
		return nil
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	return []string{
		strings.TrimSpace(call.BridgeID),
		strings.TrimSpace(call.CaptureBridgeID),
		strings.TrimSpace(call.PlaybackBridgeID),
	}
}

func cancelCall(call *Call) {
	if call == nil {
		return
	}
	call.mu.Lock()
	cancel := call.cancel
	call.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) updateCall(call *Call, status string, errText string) {
	if call == nil {
		return
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	call.Status = status
	call.Error = strings.TrimSpace(errText)
	call.UpdatedAt = time.Now().UTC()
	if (status == "failed" || status == "finished") && call.FinishedAt == nil {
		now := call.UpdatedAt
		call.FinishedAt = &now
	}
}

func (s *Server) touchCall(call *Call) {
	if call == nil {
		return
	}
	stats, ok := collectCallRTPStats(call)
	call.mu.Lock()
	defer call.mu.Unlock()
	if ok {
		applyCallRTPStatsLocked(call, stats)
	}
	call.UpdatedAt = time.Now().UTC()
}

func (s *Server) syncCallStats(call *Call) {
	if call == nil {
		return
	}
	stats, ok := collectCallRTPStats(call)
	if !ok {
		return
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	applyCallRTPStatsLocked(call, stats)
}

type callRTPStatsSet struct {
	RTP         rtpStats
	RTPCapture  rtpStats
	RTPPlayback rtpStats
}

func collectCallRTPStats(call *Call) (callRTPStatsSet, bool) {
	if call == nil {
		return callRTPStatsSet{}, false
	}
	call.mu.Lock()
	rtp := call.rtp
	rtpIn := call.rtpIn
	rtpOut := call.rtpOut
	call.mu.Unlock()

	if rtp != nil {
		stats := rtp.Stats()
		return callRTPStatsSet{RTP: stats, RTPCapture: stats}, true
	}
	if rtpIn == nil && rtpOut == nil {
		return callRTPStatsSet{}, false
	}
	stats := combineRTPStats(rtpIn, rtpOut)
	out := callRTPStatsSet{RTP: stats}
	if rtpIn != nil {
		out.RTPCapture = rtpIn.Stats()
	}
	if rtpOut != nil {
		out.RTPPlayback = rtpOut.Stats()
	}
	return out, true
}

func applyCallRTPStatsLocked(call *Call, stats callRTPStatsSet) {
	call.RTP = stats.RTP
	call.RTPCapture = stats.RTPCapture
	call.RTPPlayback = stats.RTPPlayback
}

func (s *Server) setCallChannelID(call *Call, channelID string) {
	if call == nil {
		return
	}
	call.mu.Lock()
	call.ChannelID = strings.TrimSpace(channelID)
	call.UpdatedAt = time.Now().UTC()
	call.mu.Unlock()
}

func (s *Server) setCallAnswered(call *Call, answeredAt time.Time) {
	if call == nil {
		return
	}
	if answeredAt.IsZero() {
		answeredAt = time.Now().UTC()
	}
	call.mu.Lock()
	call.AnsweredAt = cloneTimePtr(&answeredAt)
	call.UpdatedAt = answeredAt
	call.mu.Unlock()
}

func (s *Server) setCallRealtimeSession(call *Call, realtimeSessionID string, coreSessionID string) {
	if call == nil {
		return
	}
	call.mu.Lock()
	call.RealtimeSessionID = strings.TrimSpace(realtimeSessionID)
	call.CoreSessionID = strings.TrimSpace(coreSessionID)
	call.UpdatedAt = time.Now().UTC()
	call.mu.Unlock()
}

func (s *Server) setCallRTPSessions(call *Call, captureRTP *rtpSession, playbackRTP *rtpSession) {
	if call == nil {
		return
	}
	call.mu.Lock()
	call.rtpIn = captureRTP
	call.rtpOut = playbackRTP
	call.UpdatedAt = time.Now().UTC()
	call.mu.Unlock()
}

func (s *Server) clearCallRTPSessions(call *Call) {
	if call == nil {
		return
	}
	stats, ok := collectCallRTPStats(call)
	call.mu.Lock()
	if ok {
		applyCallRTPStatsLocked(call, stats)
	}
	call.rtp = nil
	call.rtpIn = nil
	call.rtpOut = nil
	call.UpdatedAt = time.Now().UTC()
	call.mu.Unlock()
}

func (s *Server) setCallBridgeIDs(call *Call, ids callBridgeIDs) {
	if call == nil {
		return
	}
	call.mu.Lock()
	call.ChannelID = strings.TrimSpace(ids.ChannelID)
	call.BridgeID = strings.TrimSpace(ids.BridgeID)
	call.ExternalChannelID = strings.TrimSpace(ids.ExternalChannelID)
	call.CaptureBridgeID = strings.TrimSpace(ids.CaptureBridgeID)
	call.PlaybackBridgeID = strings.TrimSpace(ids.PlaybackBridgeID)
	call.CaptureSnoopChannelID = strings.TrimSpace(ids.CaptureSnoopChannelID)
	call.PlaybackSnoopChannelID = strings.TrimSpace(ids.PlaybackSnoopChannelID)
	call.CaptureExternalChannelID = strings.TrimSpace(ids.CaptureExternalChannelID)
	call.PlaybackExternalChannelID = strings.TrimSpace(ids.PlaybackExternalChannelID)
	call.UpdatedAt = time.Now().UTC()
	call.mu.Unlock()
}

func (s *Server) setCallRecording(call *Call, recording *CallRecording) {
	if call == nil {
		return
	}
	call.mu.Lock()
	call.Recording = recording
	call.UpdatedAt = time.Now().UTC()
	call.mu.Unlock()
}

func (s *Server) updateCallRecording(call *Call, recording *CallRecording, update func(*CallRecording)) {
	if call == nil || recording == nil || update == nil {
		return
	}
	call.mu.Lock()
	if call.Recording == recording {
		update(recording)
		call.UpdatedAt = time.Now().UTC()
	}
	call.mu.Unlock()
}

func callRecordingRefSnapshot(call *Call, recording *CallRecording) (CallRecording, bool) {
	if call == nil || recording == nil {
		return CallRecording{}, false
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	if call.Recording != recording {
		return CallRecording{}, false
	}
	return *recording, true
}

func callRecordingSnapshotLocked(call *Call) *CallRecording {
	if call == nil || call.Recording == nil {
		return nil
	}
	copy := *call.Recording
	return &copy
}

type callBridgeIDs struct {
	ChannelID                 string
	BridgeID                  string
	ExternalChannelID         string
	CaptureBridgeID           string
	PlaybackBridgeID          string
	CaptureSnoopChannelID     string
	PlaybackSnoopChannelID    string
	CaptureExternalChannelID  string
	PlaybackExternalChannelID string
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

func (s *Server) callList() []*Call {
	s.mu.RLock()
	defer s.mu.RUnlock()
	calls := make([]*Call, 0, len(s.calls))
	for _, call := range s.calls {
		calls = append(calls, call)
	}
	return calls
}

func (s *Server) pruneFinishedCalls(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, call := range s.calls {
		if call == nil {
			delete(s.calls, id)
			continue
		}
		call.mu.Lock()
		finishedAt := cloneTimePtr(call.FinishedAt)
		call.mu.Unlock()
		if finishedAt != nil && now.Sub(*finishedAt) > finishedCallRetention {
			delete(s.calls, id)
		}
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
