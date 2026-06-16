package gateway

import (
	"context"
	"strings"
	"sync"
	"time"
)

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
