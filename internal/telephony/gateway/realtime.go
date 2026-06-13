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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
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

type mediaGate struct {
	suppressUntilUnixNano atomic.Int64
	modelActive           atomic.Bool
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

func rtpToRealtime(ctx context.Context, rtp *rtpSession, realtime *realtimeConn, gate *mediaGate) error {
	const (
		speechAvgThreshold  = 220
		speechPeakThreshold = 1500
		speechPeakMinAvg    = 120
		startSpeechFrames   = 5
		prebufferFrames     = 20
		trailingInputFrames = 12
		endSilenceFrames    = 22
	)
	var active bool
	silenceFrames := 0
	speechStartFrames := 0
	turnSpeechFrames := 0
	turnInputFrames := 0
	prebuffer := make([][]byte, 0, prebufferFrames)
	resetTurn := func() {
		active = false
		silenceFrames = 0
		speechStartFrames = 0
		turnSpeechFrames = 0
		turnInputFrames = 0
		prebuffer = prebuffer[:0]
	}
	endTurn := func(ctx context.Context) error {
		if active {
			if err := realtime.SendAudioEnd(ctx); err != nil {
				return err
			}
			log.Printf("telephony realtime input turn committed session=%s frames=%d speech_frames=%d duration_ms=%d", realtime.Session.ID, turnInputFrames, turnSpeechFrames, turnInputFrames*20)
		}
		resetTurn()
		return nil
	}
	for {
		pcm8k, err := rtp.ReadPCM8k(ctx)
		if err != nil {
			return err
		}
		if gate.Suppressing() {
			resetTurn()
			continue
		}
		pcmInput := pcm16kBytesFromPCM8k(pcm8k)
		speech := audioLooksLikeSpeech(pcm8k, speechAvgThreshold, speechPeakThreshold, speechPeakMinAvg)
		if !active {
			if len(prebuffer) >= prebufferFrames {
				copy(prebuffer, prebuffer[1:])
				prebuffer = prebuffer[:prebufferFrames-1]
			}
			prebuffer = append(prebuffer, pcmInput)
			if speech {
				speechStartFrames++
			} else {
				speechStartFrames = 0
			}
			if speechStartFrames < startSpeechFrames {
				continue
			}
			active = true
			for _, frame := range prebuffer {
				if err := realtime.SendAudioPCM(ctx, frame, 16000); err != nil {
					return err
				}
				turnInputFrames++
			}
			prebuffer = prebuffer[:0]
			turnSpeechFrames = speechStartFrames
			silenceFrames = 0
			continue
		}
		if speech {
			if err := realtime.SendAudioPCM(ctx, pcmInput, 16000); err != nil {
				return err
			}
			turnInputFrames++
			turnSpeechFrames++
			silenceFrames = 0
			continue
		}
		silenceFrames++
		if silenceFrames <= trailingInputFrames {
			if err := realtime.SendAudioPCM(ctx, pcmInput, 16000); err != nil {
				return err
			}
			turnInputFrames++
		}
		if silenceFrames >= endSilenceFrames {
			if err := endTurn(ctx); err != nil {
				return err
			}
		}
	}
}

func realtimeToRTP(ctx context.Context, realtimeConn *realtimeConn, rtp *rtpSession, call *Call, gate *mediaGate) error {
	outbound := newOutboundAudioFilter(rtp, gate)
	resampler := newPCM24ToPCM8Resampler()
	defer gate.SetModelActive(false)
	for {
		event, err := realtimeConn.Read(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case realtime.EventAssistantAudioDelta:
			gate.SetModelActive(true)
			var payload realtime.AssistantAudioPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			audio, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.AudioBase64))
			if err != nil {
				return err
			}
			pcm8k := resampler.Convert(audio)
			if err := outbound.Write(ctx, pcm8k); err != nil {
				return err
			}
		case realtime.EventInputTranscriptDelta:
			appendTranscript(call, event.Payload, true, false)
		case realtime.EventInputTranscriptFinal:
			appendTranscript(call, event.Payload, true, true)
		case realtime.EventAssistantTranscriptDelta:
			gate.SetModelActive(true)
			appendTranscript(call, event.Payload, false, false)
		case realtime.EventAssistantTranscriptFinal:
			appendTranscript(call, event.Payload, false, true)
		case realtime.EventTurnFinal:
			appendTranscriptTurn(call, event.Payload, event.At)
			if err := outbound.Write(ctx, resampler.Flush()); err != nil {
				return err
			}
			if err := outbound.EndTurn(ctx); err != nil {
				return err
			}
			gate.SetModelActive(false)
			gate.SuppressFor(postPlaybackSuppress)
		case realtime.EventInterrupted:
			outbound.Interrupt()
			resampler.Flush()
			gate.SetModelActive(false)
			gate.SuppressFor(postPlaybackSuppress)
		case realtime.EventError:
			return fmt.Errorf("realtime error: %s", string(event.Payload))
		case realtime.EventSessionClosed:
			return nil
		}
	}
}

type outboundAudioFilter struct {
	rtp     *rtpSession
	gate    *mediaGate
	pending []int16
	quiet   [][]int16
	active  bool
}

func newOutboundAudioFilter(rtp *rtpSession, gate *mediaGate) *outboundAudioFilter {
	return &outboundAudioFilter{
		rtp:   rtp,
		gate:  gate,
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
	if f.gate != nil {
		f.gate.SuppressFor(samplesDuration(len(frame), 8000) + postPlaybackSuppress)
	}
	return f.rtp.SendPCM8k(ctx, frame)
}

const (
	outboundQuietAvgThreshold  = 180
	outboundQuietPeakThreshold = 1800
	outboundTailFrames         = 8
	postPlaybackSuppress       = 250 * time.Millisecond
)

func audioFrameQuiet(samples []int16) bool {
	if len(samples) == 0 {
		return true
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
	avg := sum / len(samples)
	return avg < outboundQuietAvgThreshold || (avg < outboundQuietAvgThreshold+80 && peak < outboundQuietPeakThreshold)
}

func (g *mediaGate) SuppressFor(duration time.Duration) {
	if g == nil || duration <= 0 {
		return
	}
	until := time.Now().Add(duration).UnixNano()
	for {
		current := g.suppressUntilUnixNano.Load()
		if current >= until || g.suppressUntilUnixNano.CompareAndSwap(current, until) {
			return
		}
	}
}

func (g *mediaGate) SetModelActive(active bool) {
	if g != nil {
		g.modelActive.Store(active)
	}
}

func (g *mediaGate) Suppressing() bool {
	if g == nil {
		return false
	}
	if g.modelActive.Load() {
		return true
	}
	return time.Now().UnixNano() < g.suppressUntilUnixNano.Load()
}

func audioLooksLikeSpeech(samples []int16, avgThreshold int, peakThreshold int, peakMinAvg int) bool {
	if len(samples) == 0 {
		return false
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
	avg := sum / len(samples)
	return avg >= avgThreshold || (avg >= peakMinAvg && peak >= peakThreshold)
}

func samplesDuration(samples int, sampleRate int) time.Duration {
	if samples <= 0 || sampleRate <= 0 {
		return 0
	}
	return time.Duration(samples) * time.Second / time.Duration(sampleRate)
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
