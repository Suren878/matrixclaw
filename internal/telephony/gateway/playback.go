package gateway

import (
	"context"
	"sync"

	"github.com/Suren878/matrixclaw/internal/safego"
)

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
