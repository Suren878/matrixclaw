package gateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

func (s *Server) startCall(parent context.Context, req createCallRequest) (CallSnapshot, error) {
	_ = parent
	s.pruneFinishedCalls(time.Now().UTC())
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
	ctx, cancel := s.callContext()
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
	safego.Go("telephony.runCall", func() { s.runCall(ctx, call, req) })
	return callSnapshot(call), nil
}

func (s *Server) runCall(ctx context.Context, call *Call, req createCallRequest) {
	defer cancelCall(call)
	defer s.postCallReport(context.Background(), call, req)
	if err := s.runCallOnce(ctx, call, req); err != nil {
		s.updateCall(call, "failed", err.Error())
		log.Printf("telephony call %s failed: %v", callID(call), err)
	}
}

func (s *Server) runCallOnce(ctx context.Context, call *Call, req createCallRequest) error {
	id := callID(call)
	callID := safeARIID(id + "-call")
	s.setCallChannelID(call, callID)

	s.updateCall(call, "preparing", "")

	events, unsubscribe := s.events.Subscribe(128)
	defer unsubscribe()
	if err := s.events.WaitReady(ctx); err != nil {
		return fmt.Errorf("wait for ARI app: %w", err)
	}

	s.updateCall(call, "dialing", "")

	snapshot := callSnapshot(call)
	endpoint := fmt.Sprintf("PJSIP/%s@%s", snapshot.To, firstNonEmpty(snapshot.Profile, s.cfg.SIPProfile))
	if err := s.ari.originate(ctx, originateRequest{
		ChannelID: callID,
		Endpoint:  endpoint,
		App:       s.cfg.ARIApp,
		AppArgs:   id,
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
	s.setCallAnswered(call, time.Now().UTC())
	s.updateCall(call, "answered", "")
	logCallTimeline(call, "", "answered", "direction", callDirection(call), "channel", callID)

	realtime, err := s.connectRealtime(ctx, call, req)
	if err != nil {
		_ = s.ari.hangup(context.Background(), callID)
		return err
	}
	return s.runConnectedCallWithRealtime(ctx, call, req, callID, realtime)
}
