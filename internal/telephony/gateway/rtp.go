package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	rtpHeaderSize      = 12
	rtpPayloadTypeALaw = 8
	rtpFrameSamples    = 160
)

type rtpSession struct {
	conn         *net.UDPConn
	externalHost string
	mu           sync.RWMutex
	remote       *net.UDPAddr
	seq          uint16
	timestamp    uint32
	ssrc         uint32
	inPackets    atomic.Uint64
	inBytes      atomic.Uint64
	inSamples    atomic.Uint64
	inAbsSum     atomic.Uint64
	inPeak       atomic.Uint64
	speechFrames atomic.Uint64
	outPackets   atomic.Uint64
	outBytes     atomic.Uint64
}

type rtpStats struct {
	InPackets    uint64 `json:"in_packets"`
	InBytes      uint64 `json:"in_bytes"`
	InAvgAbs     uint64 `json:"in_avg_abs,omitempty"`
	InPeak       uint64 `json:"in_peak,omitempty"`
	SpeechFrames uint64 `json:"speech_frames,omitempty"`
	OutPackets   uint64 `json:"out_packets"`
	OutBytes     uint64 `json:"out_bytes"`
	Remote       string `json:"remote,omitempty"`
}

func newRTPSession(bind string, externalAddress func(int) string) (*rtpSession, error) {
	addr, err := net.ResolveUDPAddr("udp", bind)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("RTP listener did not return UDP address")
	}
	return &rtpSession{
		conn:         conn,
		externalHost: externalAddress(local.Port),
		ssrc:         uint32(time.Now().UnixNano()),
	}, nil
}

func (s *rtpSession) ExternalHost() string {
	if s == nil {
		return ""
	}
	return s.externalHost
}

func (s *rtpSession) Close() {
	if s != nil && s.conn != nil {
		_ = s.conn.Close()
	}
}

func (s *rtpSession) SetRemote(addr *net.UDPAddr) {
	if s == nil || addr == nil {
		return
	}
	s.mu.Lock()
	s.remote = addr
	s.mu.Unlock()
}

func (s *rtpSession) Stats() rtpStats {
	if s == nil {
		return rtpStats{}
	}
	s.mu.RLock()
	remote := ""
	if s.remote != nil {
		remote = s.remote.String()
	}
	s.mu.RUnlock()
	samples := s.inSamples.Load()
	avgAbs := uint64(0)
	if samples > 0 {
		avgAbs = s.inAbsSum.Load() / samples
	}
	return rtpStats{
		InPackets:    s.inPackets.Load(),
		InBytes:      s.inBytes.Load(),
		InAvgAbs:     avgAbs,
		InPeak:       s.inPeak.Load(),
		SpeechFrames: s.speechFrames.Load(),
		OutPackets:   s.outPackets.Load(),
		OutBytes:     s.outBytes.Load(),
		Remote:       remote,
	}
}

func (s *rtpSession) ReadPCM8k(ctx context.Context) ([]int16, error) {
	buf := make([]byte, 1500)
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
			return nil, err
		}
		if addr != nil {
			s.SetRemote(addr)
		}
		payload, ok := rtpPayload(buf[:n])
		if !ok || len(payload) == 0 {
			continue
		}
		s.inPackets.Add(1)
		s.inBytes.Add(uint64(len(payload)))
		pcm := make([]int16, len(payload))
		sumAbs := uint64(0)
		peak := uint64(0)
		for i, value := range payload {
			sample := alawDecode(value)
			pcm[i] = sample
			abs := int(sample)
			if abs < 0 {
				abs = -abs
			}
			sumAbs += uint64(abs)
			if uint64(abs) > peak {
				peak = uint64(abs)
			}
		}
		s.inSamples.Add(uint64(len(pcm)))
		s.inAbsSum.Add(sumAbs)
		s.updateInPeak(peak)
		if len(pcm) > 0 && (sumAbs/uint64(len(pcm)) >= 260 || peak >= 1100) {
			s.speechFrames.Add(1)
		}
		return pcm, nil
	}
}

func (s *rtpSession) updateInPeak(value uint64) {
	for {
		current := s.inPeak.Load()
		if current >= value || s.inPeak.CompareAndSwap(current, value) {
			return
		}
	}
}

func (s *rtpSession) SendPCM8k(ctx context.Context, pcm []int16) error {
	if len(pcm) == 0 {
		return nil
	}
	for offset := 0; offset < len(pcm); offset += rtpFrameSamples {
		end := offset + rtpFrameSamples
		if end > len(pcm) {
			end = len(pcm)
		}
		payload := make([]byte, rtpFrameSamples)
		for i := 0; i < rtpFrameSamples; i++ {
			if offset+i < end {
				payload[i] = alawEncode(pcm[offset+i])
			} else {
				payload[i] = alawEncode(0)
			}
		}
		if err := s.writeRTP(ctx, payload); err != nil {
			return err
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *rtpSession) writeRTP(ctx context.Context, payload []byte) error {
	remote, err := s.waitRemote(ctx)
	if err != nil {
		return err
	}
	packet := make([]byte, rtpHeaderSize+len(payload))
	packet[0] = 0x80
	packet[1] = rtpPayloadTypeALaw
	binary.BigEndian.PutUint16(packet[2:4], s.seq)
	binary.BigEndian.PutUint32(packet[4:8], s.timestamp)
	binary.BigEndian.PutUint32(packet[8:12], s.ssrc)
	copy(packet[rtpHeaderSize:], payload)
	s.seq++
	s.timestamp += rtpFrameSamples
	_, err = s.conn.WriteToUDP(packet, remote)
	if err == nil {
		s.outPackets.Add(1)
		s.outBytes.Add(uint64(len(payload)))
	}
	return err
}

func (s *rtpSession) waitRemote(ctx context.Context) (*net.UDPAddr, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.RLock()
		remote := s.remote
		s.mu.RUnlock()
		if remote != nil {
			return remote, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func rtpPayload(packet []byte) ([]byte, bool) {
	if len(packet) < rtpHeaderSize {
		return nil, false
	}
	version := packet[0] >> 6
	if version != 2 {
		return nil, false
	}
	csrcCount := int(packet[0] & 0x0f)
	offset := rtpHeaderSize + csrcCount*4
	if len(packet) < offset {
		return nil, false
	}
	if packet[0]&0x10 != 0 {
		if len(packet) < offset+4 {
			return nil, false
		}
		extWords := int(binary.BigEndian.Uint16(packet[offset+2 : offset+4]))
		offset += 4 + extWords*4
		if len(packet) < offset {
			return nil, false
		}
	}
	return packet[offset:], true
}

func parseUDPHostPort(host string, port string) (*net.UDPAddr, error) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" || port == "" {
		return nil, fmt.Errorf("invalid UDP address %q:%q", host, port)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, err
	}
	return net.ResolveUDPAddr("udp", net.JoinHostPort(host, port))
}
