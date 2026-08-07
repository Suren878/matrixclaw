package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestRTPPlaybackRunErrorRemainsObservable(t *testing.T) {
	want := errors.New("playback failed")
	playback := &rtpPlayback{done: make(chan struct{}), runErr: want}
	close(playback.done)
	for attempt := 0; attempt < 2; attempt++ {
		if err := playback.doneErr(); !errors.Is(err, want) {
			t.Fatalf("doneErr attempt %d = %v, want %v", attempt+1, err, want)
		}
	}
}

func TestOutboundAudioFilterSendsPrerollBeforeFirstSpeech(t *testing.T) {
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen receiver UDP: %v", err)
	}
	defer func() { _ = receiver.Close() }()

	rtp, err := newRTPSession("127.0.0.1:0", func(int) string { return "" })
	if err != nil {
		t.Fatalf("new RTP session: %v", err)
	}
	defer rtp.Close()
	rtp.SetRemote(receiver.LocalAddr().(*net.UDPAddr))

	filter := newOutboundAudioFilter(rtp)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := filter.Write(ctx, constantPCMFrame(4000)); err != nil {
		t.Fatalf("write first speech frame: %v", err)
	}

	if got := rtp.Stats().OutPackets; got <= 1 {
		t.Fatalf("out packets = %d, want initial RTP preroll before first speech", got)
	}
}

func constantPCMFrame(value int16) []int16 {
	frame := make([]int16, rtpFrameSamples)
	for i := range frame {
		frame[i] = value
	}
	return frame
}
