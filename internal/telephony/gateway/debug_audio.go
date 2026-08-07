package gateway

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const debugAudioSampleRateHz = 8000

type rtpDebugAudio struct {
	mu             sync.Mutex
	file           *os.File
	path           string
	sampleRate     int
	maxSamples     int
	writtenSamples int
	closed         bool
}

func newRTPDebugAudio(dir string, callID string, sessionID string, label string, sampleRate int, maxDuration time.Duration) (*rtpDebugAudio, error) {
	if sampleRate <= 0 {
		sampleRate = debugAudioSampleRateHz
	}
	if maxDuration <= 0 {
		maxDuration = 20 * time.Second
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, strings.Join([]string{
		debugAudioPathSegment(callID),
		debugAudioPathSegment(sessionID),
		debugAudioPathSegment(label),
	}, "_")+".wav")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	debug := &rtpDebugAudio{
		file:       file,
		path:       path,
		sampleRate: sampleRate,
		maxSamples: int(maxDuration * time.Duration(sampleRate) / time.Second),
	}
	if debug.maxSamples <= 0 {
		debug.maxSamples = sampleRate
	}
	if err := debug.writeHeader(sampleRate, 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	return debug, nil
}

func (d *rtpDebugAudio) Path() string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.path
}

func (d *rtpDebugAudio) WritePCM(samples []int16) {
	if d == nil || len(samples) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.file == nil || d.writtenSamples >= d.maxSamples {
		return
	}
	remaining := d.maxSamples - d.writtenSamples
	if len(samples) > remaining {
		samples = samples[:remaining]
	}
	buf := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(sample))
	}
	written, _ := d.file.Write(buf)
	d.writtenSamples += written / 2
}

func (d *rtpDebugAudio) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if d.file == nil {
		return nil
	}
	dataBytes := d.writtenSamples * 2
	if _, err := d.file.Seek(0, 0); err != nil {
		_ = d.file.Close()
		return err
	}
	if err := d.writeHeader(d.sampleRate, dataBytes); err != nil {
		_ = d.file.Close()
		return err
	}
	return d.file.Close()
}

func (d *rtpDebugAudio) writeHeader(sampleRate int, dataBytes int) error {
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataBytes))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataBytes))
	_, err := d.file.Write(header)
	return err
}

func debugAudioPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
