package gateway

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

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
	safego.Go("telephony.runInboundCall", func() { s.runInboundCall(ctx, call) })
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
