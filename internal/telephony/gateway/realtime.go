package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/safego"
	"nhooyr.io/websocket"
)

type realtimeClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type realtimeConn struct {
	Session realtime.SessionInfo
	conn    *websocket.Conn
	writeMu sync.Mutex
}

type realtimeConnectRequest struct {
	Client            string
	ExternalKey       string
	SessionID         string
	SystemInstruction string
}

func newRealtimeClient(baseURL string, token string) *realtimeClient {
	return &realtimeClient{
		baseURL: trimRightSlash(firstNonEmpty(baseURL, defaultMatrixclawURL)),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *realtimeClient) Connect(ctx context.Context, input realtimeConnectRequest) (*realtimeConn, error) {
	create := realtime.SessionCreateRequest{
		Client:            firstNonEmpty(input.Client, "telephony"),
		ExternalKey:       strings.TrimSpace(input.ExternalKey),
		SessionID:         strings.TrimSpace(input.SessionID),
		SystemInstruction: strings.TrimSpace(input.SystemInstruction),
		InputAudio:        realtime.DefaultInputAudioFormat(),
		OutputAudio:       realtime.DefaultOutputAudioFormat(),
		PersistMode:       realtime.PersistModeTurnsAndSummary,
	}
	payload, err := json.Marshal(create)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/realtime-voice/sessions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("create realtime session returned HTTP %d", res.StatusCode)
	}
	var created realtime.SessionCreateResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		return nil, err
	}
	wsURL, err := c.streamURL(created.Session.ID)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	if c.token != "" {
		headers.Set("Authorization", "Bearer "+c.token)
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	out := &realtimeConn{Session: created.Session, conn: conn}
	if err := out.waitReady(ctx); err != nil {
		_ = out.Close(context.Background())
		return nil, err
	}
	return out, nil
}

func (c *realtimeClient) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *realtimeClient) streamURL(sessionID string) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported MatrixClaw URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/realtime-voice/sessions/" + url.PathEscape(sessionID) + "/stream"
	return parsed.String(), nil
}

func (c *realtimeConn) waitReady(ctx context.Context) error {
	for {
		event, err := c.Read(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case realtime.EventSessionReady:
			return nil
		case realtime.EventError:
			return fmt.Errorf("realtime error before ready: %s", string(event.Payload))
		}
	}
}

func (c *realtimeConn) Read(ctx context.Context) (realtime.Event, error) {
	for {
		messageType, data, err := c.conn.Read(ctx)
		if err != nil {
			return realtime.Event{}, err
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		var event realtime.Event
		if err := json.Unmarshal(data, &event); err != nil {
			return realtime.Event{}, err
		}
		return event, nil
	}
}

func (c *realtimeConn) SendAudioPCM(ctx context.Context, pcm []byte, sampleRateHz int) error {
	if len(pcm) == 0 {
		return nil
	}
	if sampleRateHz <= 0 {
		sampleRateHz = 16000
	}
	return c.writeEvent(ctx, realtime.EventInputAudioAppend, realtime.InputAudioPayload{
		AudioBase64: base64.StdEncoding.EncodeToString(pcm),
		MIMEType:    fmt.Sprintf("audio/pcm;rate=%d", sampleRateHz),
		DurationMs:  len(pcm) / 2 * 1000 / sampleRateHz,
	})
}

func (c *realtimeConn) SendAudioEnd(ctx context.Context) error {
	return c.writeEvent(ctx, realtime.EventInputAudioEnd, map[string]any{})
}

func (c *realtimeConn) SendText(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return c.writeEvent(ctx, realtime.EventInputTextAppend, realtime.InputTextPayload{Text: text, EndOfTurn: true})
}

func (c *realtimeConn) Close(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return nil
	}
	_ = c.writeEvent(ctx, realtime.EventSessionClose, map[string]any{})
	return c.conn.Close(websocket.StatusNormalClosure, "done")
}

func (c *realtimeConn) writeEvent(ctx context.Context, eventType realtime.EventType, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := realtime.Event{
		V:              realtime.ProtocolVersion,
		Type:           eventType,
		VoiceSessionID: c.Session.ID,
		Payload:        body,
		At:             time.Now().UTC(),
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func rtpToRealtime(ctx context.Context, rtp *rtpSession, realtime *realtimeConn, call *Call) error {
	var active bool
	var startFrames int
	var silenceFrames int
	var sentFrames int
	var activityFrames int
	prebuffer := make([][]byte, 0, inputPrebufferFrames)
	reset := func() {
		active = false
		startFrames = 0
		silenceFrames = 0
		sentFrames = 0
		activityFrames = 0
		prebuffer = prebuffer[:0]
	}
	send := func(ctx context.Context, pcm []byte) error {
		if err := realtime.SendAudioPCM(ctx, pcm, 16000); err != nil {
			return err
		}
		sentFrames++
		return nil
	}
	for {
		pcm8k, err := rtp.ReadPCM8k(ctx)
		if err != nil {
			return err
		}
		pcmInput := pcm16kBytesFromPCM8k(pcm8k)
		level := audioFrameLevel(pcm8k)
		activity := inputLevelHasActivity(level)
		if !active {
			if len(prebuffer) >= inputPrebufferFrames {
				copy(prebuffer, prebuffer[1:])
				prebuffer = prebuffer[:inputPrebufferFrames-1]
			}
			prebuffer = append(prebuffer, pcmInput)
			if activity {
				startFrames++
			} else {
				startFrames = 0
			}
			if startFrames < inputStartActivityFrames {
				continue
			}
			active = true
			silenceFrames = 0
			activityFrames = startFrames
			sentFrames = 0
			log.Printf("telephony realtime input activity started call=%s session=%s avg=%d peak=%d prebuffer_frames=%d", callID(call), realtime.Session.ID, level.avg, level.peak, len(prebuffer))
			logCallTimeline(call, realtime.Session.ID, "input_activity_start", "avg", level.avg, "peak", level.peak, "prebuffer_frames", len(prebuffer))
			for _, frame := range prebuffer {
				if err := send(ctx, frame); err != nil {
					return err
				}
			}
			prebuffer = prebuffer[:0]
			continue
		}
		if err := send(ctx, pcmInput); err != nil {
			return err
		}
		if activity {
			activityFrames++
			silenceFrames = 0
			continue
		}
		silenceFrames++
		if silenceFrames >= inputEndSilenceFrames {
			if err := realtime.SendAudioEnd(ctx); err != nil {
				return err
			}
			log.Printf("telephony realtime input stream ended call=%s session=%s frames=%d activity_frames=%d duration_ms=%d", callID(call), realtime.Session.ID, sentFrames, activityFrames, sentFrames*20)
			logCallTimeline(call, realtime.Session.ID, "input_stream_end", "frames", sentFrames, "activity_frames", activityFrames, "duration_ms", sentFrames*20)
			reset()
		}
	}
}

func realtimeToRTP(ctx context.Context, realtimeConn *realtimeConn, rtp *rtpSession, call *Call) error {
	playback := newRTPPlayback(ctx, rtp)
	defer func() {
		_ = playback.Close(context.Background())
	}()
	outputRate := realtime.DefaultOutputAudioFormat().SampleRateHz
	if realtimeConn != nil && realtimeConn.Session.OutputAudio.SampleRateHz > 0 {
		outputRate = realtimeConn.Session.OutputAudio.SampleRateHz
	}
	resampler := newPCMToPCM8Resampler(outputRate)
	assistantAudioActive := false
	for {
		event, err := realtimeConn.Read(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case realtime.EventAssistantAudioDelta:
			var payload realtime.AssistantAudioPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			audio, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.AudioBase64))
			if err != nil {
				return err
			}
			if !assistantAudioActive {
				assistantAudioActive = true
				log.Printf("telephony realtime assistant audio started call=%s session=%s mime=%q bytes=%d", callID(call), realtimeConn.Session.ID, payload.MIMEType, len(audio))
				logCallTimeline(call, realtimeConn.Session.ID, "assistant_audio_delta", "mime", payload.MIMEType, "bytes", len(audio))
			}
			if rate := audioSampleRateFromMIME(payload.MIMEType); rate > 0 && rate != resampler.InputRate() {
				if err := playback.Write(ctx, resampler.Flush()); err != nil {
					return err
				}
				resampler = newPCMToPCM8Resampler(rate)
			}
			pcm8k := resampler.Convert(audio)
			if err := playback.Write(ctx, pcm8k); err != nil {
				return err
			}
		case realtime.EventInputTranscriptDelta:
			appendTranscript(call, event.Payload, true, false)
		case realtime.EventInputTranscriptFinal:
			appendTranscript(call, event.Payload, true, true)
		case realtime.EventAssistantTranscriptDelta:
			appendTranscript(call, event.Payload, false, false)
		case realtime.EventAssistantTranscriptFinal:
			appendTranscript(call, event.Payload, false, true)
		case realtime.EventTurnFinal:
			appendTranscriptTurn(call, event.Payload, event.At)
			assistantAudioActive = false
			if err := playback.Write(ctx, resampler.Flush()); err != nil {
				return err
			}
			if err := playback.EndTurn(ctx); err != nil {
				return err
			}
		case realtime.EventInterrupted:
			playback.Interrupt()
			resampler.Flush()
			assistantAudioActive = false
			clearCurrentAssistantTranscript(call)
		case realtime.EventError:
			if recoverableRealtimeError(event.Payload) {
				log.Printf("telephony recoverable realtime error session=%s: %s", realtimeConn.Session.ID, strings.TrimSpace(string(event.Payload)))
				continue
			}
			return fmt.Errorf("realtime error: %s", string(event.Payload))
		case realtime.EventSessionClosed:
			return nil
		}
	}
}

func recoverableRealtimeError(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var payload realtime.ErrorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	return payload.Recoverable
}

type rtpPlaybackCommandType int

const (
	rtpPlaybackAudio rtpPlaybackCommandType = iota
	rtpPlaybackEndTurn
	rtpPlaybackInterrupt
)

type rtpPlaybackCommand struct {
	kind rtpPlaybackCommandType
	pcm  []int16
}

type rtpPlayback struct {
	outbound *outboundAudioFilter
	commands chan rtpPlaybackCommand
	stop     context.CancelFunc
	done     chan error

	currentMu     sync.Mutex
	currentCancel context.CancelFunc
	currentSeq    uint64
}

func newRTPPlayback(parent context.Context, rtp *rtpSession) *rtpPlayback {
	ctx, cancel := context.WithCancel(parent)
	playback := &rtpPlayback{
		outbound: newOutboundAudioFilter(rtp),
		commands: make(chan rtpPlaybackCommand, 32),
		stop:     cancel,
		done:     make(chan error, 1),
	}
	safego.Go("telephony.rtpPlayback.run", func() { playback.run(ctx) })
	return playback
}

func (p *rtpPlayback) Write(ctx context.Context, pcm []int16) error {
	if p == nil || len(pcm) == 0 {
		return nil
	}
	return p.enqueue(ctx, rtpPlaybackCommand{kind: rtpPlaybackAudio, pcm: append([]int16(nil), pcm...)})
}

func (p *rtpPlayback) EndTurn(ctx context.Context) error {
	if p == nil {
		return nil
	}
	return p.enqueue(ctx, rtpPlaybackCommand{kind: rtpPlaybackEndTurn})
}

func (p *rtpPlayback) Interrupt() {
	if p == nil {
		return
	}
	p.cancelCurrent()
	for {
		select {
		case <-p.commands:
			continue
		default:
		}
		break
	}
	select {
	case p.commands <- rtpPlaybackCommand{kind: rtpPlaybackInterrupt}:
	default:
	}
}

func (p *rtpPlayback) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.stop()
	p.cancelCurrent()
	select {
	case err, ok := <-p.done:
		if ok {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *rtpPlayback) enqueue(ctx context.Context, command rtpPlaybackCommand) error {
	if err := p.doneErr(); err != nil {
		return err
	}
	for attempts := 0; attempts < 2; attempts++ {
		select {
		case p.commands <- command:
			return nil
		default:
			p.dropOldest()
		}
	}
	select {
	case p.commands <- command:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (p *rtpPlayback) dropOldest() {
	select {
	case <-p.commands:
	default:
	}
}

func (p *rtpPlayback) doneErr() error {
	select {
	case err, ok := <-p.done:
		if ok {
			return err
		}
		return nil
	default:
		return nil
	}
}

func (p *rtpPlayback) run(ctx context.Context) {
	var runErr error
	defer func() {
		if runErr != nil {
			p.done <- runErr
		}
		close(p.done)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case command := <-p.commands:
			switch command.kind {
			case rtpPlaybackAudio:
				if err := p.runCancelable(ctx, func(commandCtx context.Context) error {
					return p.outbound.Write(commandCtx, command.pcm)
				}); err != nil {
					if ctx.Err() != nil {
						return
					}
					if err == context.Canceled || err == context.DeadlineExceeded {
						p.outbound.Interrupt()
						continue
					}
					runErr = err
					return
				}
			case rtpPlaybackEndTurn:
				if err := p.runCancelable(ctx, p.outbound.EndTurn); err != nil {
					if ctx.Err() != nil {
						return
					}
					if err == context.Canceled || err == context.DeadlineExceeded {
						p.outbound.Interrupt()
						continue
					}
					runErr = err
					return
				}
			case rtpPlaybackInterrupt:
				p.outbound.Interrupt()
			}
		}
	}
}

func (p *rtpPlayback) runCancelable(parent context.Context, run func(context.Context) error) error {
	ctx, cancel := context.WithCancel(parent)
	p.currentMu.Lock()
	p.currentSeq++
	seq := p.currentSeq
	p.currentCancel = cancel
	p.currentMu.Unlock()
	defer func() {
		cancel()
		p.currentMu.Lock()
		if p.currentSeq == seq {
			p.currentCancel = nil
		}
		p.currentMu.Unlock()
	}()
	return run(ctx)
}

func (p *rtpPlayback) cancelCurrent() {
	p.currentMu.Lock()
	cancel := p.currentCancel
	p.currentMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

type outboundAudioFilter struct {
	rtp     *rtpSession
	pending []int16
	quiet   [][]int16
	active  bool
}

func newOutboundAudioFilter(rtp *rtpSession) *outboundAudioFilter {
	return &outboundAudioFilter{
		rtp:   rtp,
		quiet: make([][]int16, 0, outboundTailFrames),
	}
}

func (f *outboundAudioFilter) Write(ctx context.Context, pcm []int16) error {
	if len(pcm) == 0 {
		return nil
	}
	f.pending = append(f.pending, pcm...)
	for len(f.pending) >= rtpFrameSamples {
		frame := append([]int16(nil), f.pending[:rtpFrameSamples]...)
		f.pending = f.pending[rtpFrameSamples:]
		if err := f.writeFrame(ctx, frame); err != nil {
			return err
		}
	}
	return nil
}

func (f *outboundAudioFilter) EndTurn(ctx context.Context) error {
	if len(f.pending) > 0 {
		frame := make([]int16, rtpFrameSamples)
		copy(frame, f.pending)
		f.pending = f.pending[:0]
		if !audioFrameQuiet(frame) {
			if err := f.writeFrame(ctx, frame); err != nil {
				return err
			}
		}
	}
	f.quiet = f.quiet[:0]
	if f.active {
		if err := f.sendFrame(ctx, make([]int16, rtpFrameSamples)); err != nil {
			return err
		}
	}
	f.active = false
	return nil
}

func (f *outboundAudioFilter) Interrupt() {
	if f == nil {
		return
	}
	f.pending = f.pending[:0]
	f.quiet = f.quiet[:0]
	f.active = false
}

func (f *outboundAudioFilter) writeFrame(ctx context.Context, frame []int16) error {
	if audioFrameQuiet(frame) {
		if !f.active {
			return nil
		}
		if len(f.quiet) < outboundTailFrames {
			f.quiet = append(f.quiet, frame)
		}
		return nil
	}
	if len(f.quiet) > 0 {
		for _, quietFrame := range f.quiet {
			if err := f.sendFrame(ctx, quietFrame); err != nil {
				return err
			}
		}
		f.quiet = f.quiet[:0]
	}
	f.active = true
	return f.sendFrame(ctx, frame)
}

func (f *outboundAudioFilter) sendFrame(ctx context.Context, frame []int16) error {
	return f.rtp.SendPCM8k(ctx, frame)
}

const (
	inputActivityAvgThreshold  = 90
	inputActivityPeakThreshold = 700
	inputActivityPeakMinAvg    = 45
	inputPrebufferFrames       = 15
	inputStartActivityFrames   = 2
	inputEndSilenceFrames      = 35

	outboundQuietAvgThreshold  = 180
	outboundQuietPeakThreshold = 1800
	outboundTailFrames         = 8
)

func inputFrameHasActivity(samples []int16) bool {
	return inputLevelHasActivity(audioFrameLevel(samples))
}

func inputLevelHasActivity(level audioLevel) bool {
	return level.avg >= inputActivityAvgThreshold ||
		(level.avg >= inputActivityPeakMinAvg && level.peak >= inputActivityPeakThreshold)
}

func audioFrameQuiet(samples []int16) bool {
	level := audioFrameLevel(samples)
	return level.avg < outboundQuietAvgThreshold ||
		(level.avg < outboundQuietAvgThreshold+80 && level.peak < outboundQuietPeakThreshold)
}

type audioLevel struct {
	avg  int
	peak int
}

func audioFrameLevel(samples []int16) audioLevel {
	if len(samples) == 0 {
		return audioLevel{}
	}
	sum := 0
	peak := 0
	for _, sample := range samples {
		value := int(sample)
		if value < 0 {
			value = -value
		}
		sum += value
		if value > peak {
			peak = value
		}
	}
	return audioLevel{avg: sum / len(samples), peak: peak}
}

func audioSampleRateFromMIME(value string) int {
	for _, part := range strings.Split(value, ";") {
		key, raw, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "rate") {
			continue
		}
		rate, err := strconv.Atoi(strings.Trim(strings.TrimSpace(raw), `"`))
		if err == nil && rate > 0 {
			return rate
		}
	}
	return 0
}

func appendTranscript(call *Call, raw json.RawMessage, input bool, final bool) {
	if call == nil || len(raw) == 0 {
		return
	}
	var payload realtime.TranscriptPayload
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.Text) == "" {
		return
	}
	call.transcriptMu.Lock()
	defer call.transcriptMu.Unlock()
	if input {
		if final {
			call.currentInputTranscript = payload.Text
			call.InputTranscript = payload.Text
			return
		}
		call.currentInputTranscript += payload.Text
		call.InputTranscript = call.currentInputTranscript
	} else {
		if final {
			call.currentAssistantTranscript = payload.Text
			call.AssistantTranscript = payload.Text
			return
		}
		call.currentAssistantTranscript += payload.Text
		call.AssistantTranscript = call.currentAssistantTranscript
	}
}

func clearCurrentAssistantTranscript(call *Call) {
	if call == nil {
		return
	}
	call.transcriptMu.Lock()
	defer call.transcriptMu.Unlock()
	call.currentAssistantTranscript = ""
	call.AssistantTranscript = joinTranscript(call.Transcript, "assistant")
}

func appendTranscriptTurn(call *Call, raw json.RawMessage, at time.Time) {
	if call == nil || len(raw) == 0 {
		return
	}
	var payload realtime.TurnFinalPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	input := strings.TrimSpace(payload.InputTranscript)
	assistant := strings.TrimSpace(payload.AssistantTranscript)
	if input == "" && assistant == "" {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	call.transcriptMu.Lock()
	defer call.transcriptMu.Unlock()
	index := nextTranscriptTurnIndex(call.Transcript)
	if input != "" {
		call.Transcript = append(call.Transcript, CallTranscriptTurn{
			At:        at,
			Speaker:   "caller",
			Text:      input,
			TurnIndex: index,
		})
	}
	if assistant != "" {
		call.Transcript = append(call.Transcript, CallTranscriptTurn{
			At:        at,
			Speaker:   "assistant",
			Text:      assistant,
			TurnIndex: index,
		})
	}
	call.InputTranscript = joinTranscript(call.Transcript, "caller")
	call.AssistantTranscript = joinTranscript(call.Transcript, "assistant")
	call.currentInputTranscript = ""
	call.currentAssistantTranscript = ""
}

func joinTranscript(turns []CallTranscriptTurn, speaker string) string {
	var parts []string
	for _, turn := range turns {
		if turn.Speaker == speaker && strings.TrimSpace(turn.Text) != "" {
			parts = append(parts, strings.TrimSpace(turn.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func nextTranscriptTurnIndex(turns []CallTranscriptTurn) int {
	maxIndex := 0
	for _, turn := range turns {
		if turn.TurnIndex > maxIndex {
			maxIndex = turn.TurnIndex
		}
	}
	return maxIndex + 1
}
