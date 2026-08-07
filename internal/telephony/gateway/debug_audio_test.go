package gateway

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRTPDebugAudioWritesPCM16WAV(t *testing.T) {
	dir := t.TempDir()
	debug, err := newRTPDebugAudio(dir, "call/test", "voice/test", "capture", 8000, time.Second)
	if err != nil {
		t.Fatalf("newRTPDebugAudio returned error: %v", err)
	}
	debug.WritePCM([]int16{1, -2, 300})
	path := debug.Path()
	if err := debug.Close(); err != nil {
		t.Fatalf("debug.Close returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug wav: %v", err)
	}
	if got := string(data[:4]); got != "RIFF" {
		t.Fatalf("RIFF header = %q, want RIFF", got)
	}
	if got := string(data[8:12]); got != "WAVE" {
		t.Fatalf("WAVE header = %q, want WAVE", got)
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != 8000 {
		t.Fatalf("sample rate = %d, want 8000", got)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != 6 {
		t.Fatalf("data size = %d, want 6", got)
	}
	if got := int16(binary.LittleEndian.Uint16(data[44:46])); got != 1 {
		t.Fatalf("sample 0 = %d, want 1", got)
	}
	if got := int16(binary.LittleEndian.Uint16(data[46:48])); got != -2 {
		t.Fatalf("sample 1 = %d, want -2", got)
	}
	if filepath.Base(path) != "call_test_voice_test_capture.wav" {
		t.Fatalf("debug path base = %q", filepath.Base(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat debug wav: %v", err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("debug wav permissions = %o, want no group/other access", got)
	}
}

func TestRTPDebugAudioCapsRecordedSamples(t *testing.T) {
	debug, err := newRTPDebugAudio(t.TempDir(), "call", "voice", "playback", 8000, time.Millisecond)
	if err != nil {
		t.Fatalf("newRTPDebugAudio returned error: %v", err)
	}
	debug.WritePCM([]int16{1, 2, 3, 4, 5, 6, 7, 8, 9})
	path := debug.Path()
	if err := debug.Close(); err != nil {
		t.Fatalf("debug.Close returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug wav: %v", err)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != 16 {
		t.Fatalf("data size = %d, want 16", got)
	}
}
