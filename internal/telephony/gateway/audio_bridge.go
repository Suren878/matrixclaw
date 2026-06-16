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

func rtpToRealtime(ctx context.Context, rtp *rtpSession, realtime *realtimeConn, _ *Call) error {
	for {
		pcm8k, err := rtp.ReadPCM8k(ctx)
		if err != nil {
			return err
		}
		if err := realtime.SendAudioPCM(ctx, pcm16kBytesFromPCM8k(pcm8k), 16000); err != nil {
			return err
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
	outboundQuietAvgThreshold  = 180
	outboundQuietPeakThreshold = 1800
	outboundTailFrames         = 8
)

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
