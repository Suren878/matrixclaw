package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

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
