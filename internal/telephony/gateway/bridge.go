package gateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

func (s *Server) connectRealtime(ctx context.Context, call *Call, req createCallRequest) (*realtimeConn, error) {
	snapshot := callSnapshot(call)
	realtimeClient := newRealtimeClient(s.api)
	realtime, err := realtimeClient.Connect(ctx, realtimeConnectRequest{
		Client:      "telephony",
		ExternalKey: firstNonEmpty(req.ExternalKey, snapshot.ID),
		SessionID:   strings.TrimSpace(req.SessionID),
		SystemInstruction: phoneSystemInstruction(phonePromptInput{
			AssistantName:      firstNonEmpty(req.AssistantName, s.cfg.AssistantName),
			CallID:             snapshot.ID,
			OpeningPhrase:      req.InitialMessage,
			PhonePrompt:        firstNonEmpty(req.PhonePrompt, s.cfg.PhonePrompt),
			CustomInstructions: req.AssistantCustomInstructions,
			Objective:          firstNonEmpty(req.SystemInstruction, req.Objective),
			Direction:          snapshot.Direction,
		}),
	})
	if err != nil {
		return nil, err
	}
	s.setCallRealtimeSession(call, realtime.Session.ID, realtime.Session.CoreSessionID)
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
	defer func() { captureRTP.Close() }()
	defer func() { playbackRTP.Close() }()
	id := callID(call)
	s.setCallRTPSessions(call, captureRTP, playbackRTP)
	captureRTP.SetDiagnostics(id, realtime.Session.ID, "capture")
	playbackRTP.SetDiagnostics(id, realtime.Session.ID, "playback")
	defer func() {
		s.clearCallRTPSessions(call)
	}()

	captureBridgeID := safeARIID(id + "-capture-bridge")
	playbackBridgeID := safeARIID(id + "-playback-bridge")
	captureSnoopID := safeARIID(id + "-capture-snoop")
	captureExternalID := safeARIID(id + "-capture-media")
	playbackExternalID := safeARIID(id + "-playback-media")
	s.setCallBridgeIDs(call, callBridgeIDs{
		ChannelID:                 channelID,
		BridgeID:                  playbackBridgeID,
		ExternalChannelID:         playbackExternalID,
		CaptureBridgeID:           captureBridgeID,
		PlaybackBridgeID:          playbackBridgeID,
		CaptureSnoopChannelID:     captureSnoopID,
		PlaybackSnoopChannelID:    "",
		CaptureExternalChannelID:  captureExternalID,
		PlaybackExternalChannelID: playbackExternalID,
	})

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
		AppArgs:   id + ",capture",
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
		Data:         id + ",capture",
	}); err != nil {
		return err
	}
	if _, err := s.ari.externalMedia(ctx, externalMediaRequest{
		ChannelID:    playbackExternalID,
		App:          s.cfg.ARIApp,
		ExternalHost: playbackRTP.ExternalHost(),
		Format:       "alaw",
		Data:         id + ",playback",
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
	runCallRuntimeWorker("telephony.rtpInput", audioErr, "rtp_input", func() error {
		return rtpToRealtime(ctx, captureRTP, realtime, call)
	})
	runCallRuntimeWorker("telephony.realtimeOutput", audioErr, "realtime_output", func() error {
		return realtimeToRTP(ctx, realtime, playbackRTP, call)
	})
	runCallRuntimeWorker("telephony.ariChannel", audioErr, "ari_channel", func() error {
		return s.ari.waitChannelEnd(ctx, channelID)
	})

	s.updateCall(call, "bridged", "")
	logCallTimeline(call, realtime.Session.ID, "bridged",
		"channel", channelID,
		"capture_bridge", captureBridgeID,
		"capture_channels", captureSnoopID+"+"+captureExternalID,
		"playback_bridge", playbackBridgeID,
		"playback_channels", channelID+"+"+playbackExternalID,
	)
	if prompt := initialPhoneStartPrompt(call, req); prompt != "" {
		log.Printf("telephony sending initial realtime prompt call=%s session=%s direction=%s", id, realtime.Session.ID, callDirection(call))
		if err := realtime.SendText(ctx, prompt); err != nil {
			log.Printf("telephony initial realtime prompt failed call=%s session=%s: %v", id, realtime.Session.ID, err)
		} else {
			logCallTimeline(call, realtime.Session.ID, "initial_prompt_sent", "direction", callDirection(call), "bytes", len(prompt))
		}
	}

	select {
	case <-ctx.Done():
		logCallTimeline(call, realtime.Session.ID, "runtime_end", "source", "context", "error", ctx.Err())
	case result := <-audioErr:
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			log.Printf("telephony call runtime failed call=%s source=%s: %v", id, result.source, result.err)
			logCallTimeline(call, realtime.Session.ID, "runtime_end", "source", result.source, "error", result.err)
			finishRecording()
			cleanup()
			return result.err
		}
		log.Printf("telephony call runtime ended call=%s source=%s", id, result.source)
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
			id := callID(call)
			log.Printf("telephony RTP remote set call=%s label=%s remote=%s channel=%s", id, label, addr.String(), channelID)
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
	id := callID(call)
	if required {
		return fmt.Errorf("telephony RTP remote not available call=%s label=%s channel=%s: %w", id, label, channelID, lastErr)
	}
	log.Printf("telephony RTP remote unavailable call=%s label=%s channel=%s: %v", id, label, channelID, lastErr)
	return nil
}

type callRuntimeResult struct {
	source string
	err    error
}

func runCallRuntimeWorker(name string, out chan<- callRuntimeResult, source string, run func() error) {
	safego.Go(name, func() {
		result := callRuntimeResult{source: source}
		if !safego.Run(name+".body", func() {
			result.err = run()
		}) {
			result.err = fmt.Errorf("%s panicked", source)
		}
		out <- result
	})
}

func initialPhoneStartPrompt(call *Call, req createCallRequest) string {
	phrase := strings.TrimSpace(req.InitialMessage)
	if phrase == "" {
		phrase = "Здравствуйте."
	}
	direction := ""
	if call != nil {
		direction = callDirection(call)
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
