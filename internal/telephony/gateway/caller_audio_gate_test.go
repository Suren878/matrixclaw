package gateway

import (
	"testing"
	"time"
)

func TestCallerAudioGateStartsOpenWhenDisabled(t *testing.T) {
	gate := newCallerAudioGate(false, time.Second)
	if !gate.Allow() {
		t.Fatalf("disabled caller audio gate should allow audio")
	}
}

func TestCallerAudioGateOpensOnce(t *testing.T) {
	gate := newCallerAudioGate(true, time.Second)
	if gate.Allow() {
		t.Fatalf("enabled caller audio gate allowed audio before open")
	}
	if !gate.Open("turn_final") {
		t.Fatalf("first Open returned false")
	}
	if !gate.Allow() {
		t.Fatalf("caller audio gate did not allow audio after open")
	}
	if gate.Open("timeout") {
		t.Fatalf("second Open returned true")
	}
}

func TestCallerAudioGateTimeout(t *testing.T) {
	gate := newCallerAudioGate(true, time.Millisecond)
	defer gate.Stop()

	select {
	case <-gate.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("caller audio gate did not open after timeout")
	}
	if !gate.Allow() {
		t.Fatalf("caller audio gate did not allow audio after timeout")
	}
}

func TestCallerAudioGatePlaybackDelay(t *testing.T) {
	if got := callerAudioGatePlaybackDelay(0); got != 0 {
		t.Fatalf("callerAudioGatePlaybackDelay(0) = %v, want 0", got)
	}
	if got := callerAudioGatePlaybackDelay(8000); got != 1300*time.Millisecond {
		t.Fatalf("callerAudioGatePlaybackDelay(8000) = %v, want 1.3s", got)
	}
	if got := callerAudioGatePlaybackDelay(8000 * 20); got != 7*time.Second {
		t.Fatalf("callerAudioGatePlaybackDelay(large) = %v, want 7s cap", got)
	}
}
