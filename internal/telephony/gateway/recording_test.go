package gateway

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRecordingFormat(t *testing.T) {
	tests := map[string]string{
		"":       defaultRecordingFormat,
		"MP3":    "mp3",
		" wav ":  "wav",
		"u-law":  "ulaw",
		"flac":   defaultRecordingFormat,
		"../wav": "wav",
	}
	for input, want := range tests {
		if got := normalizeRecordingFormat(input); got != want {
			t.Fatalf("normalizeRecordingFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRecordingCaptureFormat(t *testing.T) {
	if got := recordingCaptureFormat("mp3"); got != "wav" {
		t.Fatalf("recordingCaptureFormat(mp3) = %q, want wav", got)
	}
	if got := recordingCaptureFormat("gsm"); got != "gsm" {
		t.Fatalf("recordingCaptureFormat(gsm) = %q, want gsm", got)
	}
	if got := recordingCaptureFormat("flac"); got != "wav" {
		t.Fatalf("recordingCaptureFormat(flac) = %q, want wav through default mp3", got)
	}
}

func TestRecordingMIMEType(t *testing.T) {
	tests := map[string]string{
		"mp3":  "audio/mpeg",
		"wav":  "audio/wav",
		"gsm":  "audio/gsm",
		"alaw": "audio/basic",
		"flac": "audio/mpeg",
	}
	for input, want := range tests {
		if got := recordingMIMEType(input); got != want {
			t.Fatalf("recordingMIMEType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDownloadRecordingWithBackoffRetriesStoredRecording404(t *testing.T) {
	var attempts int
	var delays []time.Duration
	data, err := downloadRecordingWithBackoff(
		context.Background(),
		"rec",
		func(context.Context, string) ([]byte, error) {
			attempts++
			if attempts < 3 {
				return nil, ariStatusError{StatusCode: http.StatusNotFound, Body: `{"message":"Recording not found"}`}
			}
			return []byte("audio"), nil
		},
		func(_ context.Context, delay time.Duration) bool {
			delays = append(delays, delay)
			return true
		},
	)
	if err != nil {
		t.Fatalf("downloadRecordingWithBackoff returned error: %v", err)
	}
	if string(data) != "audio" {
		t.Fatalf("downloadRecordingWithBackoff data = %q, want audio", string(data))
	}
	if attempts != 3 {
		t.Fatalf("download attempts = %d, want 3", attempts)
	}
	wantDelays := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("retry delays = %v, want %v", delays, wantDelays)
	}
}

func TestDownloadRecordingWithBackoffStopsOnNonRetryableError(t *testing.T) {
	boom := errors.New("boom")
	var slept bool
	_, err := downloadRecordingWithBackoff(
		context.Background(),
		"rec",
		func(context.Context, string) ([]byte, error) { return nil, boom },
		func(context.Context, time.Duration) bool {
			slept = true
			return true
		},
	)
	if !errors.Is(err, boom) {
		t.Fatalf("downloadRecordingWithBackoff error = %v, want boom", err)
	}
	if slept {
		t.Fatalf("downloadRecordingWithBackoff slept after non-retryable error")
	}
}

func TestDownloadRecordingWithBackoffReturnsRetryableAfterMaxAttempts(t *testing.T) {
	var attempts int
	_, err := downloadRecordingWithBackoff(
		context.Background(),
		"rec",
		func(context.Context, string) ([]byte, error) {
			attempts++
			return nil, ariStatusError{StatusCode: http.StatusNotFound, Body: `{"message":"Recording not found"}`}
		},
		func(context.Context, time.Duration) bool { return true },
	)
	if err == nil {
		t.Fatalf("downloadRecordingWithBackoff returned nil error")
	}
	if attempts != recordingDownloadMaxAttempts {
		t.Fatalf("download attempts = %d, want %d", attempts, recordingDownloadMaxAttempts)
	}
	if !isARIStatus(err, http.StatusNotFound) {
		t.Fatalf("downloadRecordingWithBackoff error = %v, want wrapped 404", err)
	}
	if !strings.Contains(err.Error(), "after 14 attempts") {
		t.Fatalf("downloadRecordingWithBackoff error = %q, want attempt count", err.Error())
	}
}

func TestNextRecordingDownloadBackoffCaps(t *testing.T) {
	if got := nextRecordingDownloadBackoff(0); got != recordingDownloadInitialBackoff {
		t.Fatalf("nextRecordingDownloadBackoff(0) = %v, want %v", got, recordingDownloadInitialBackoff)
	}
	if got := nextRecordingDownloadBackoff(250 * time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("nextRecordingDownloadBackoff(250ms) = %v, want 500ms", got)
	}
	if got := nextRecordingDownloadBackoff(2 * time.Second); got != recordingDownloadMaxBackoff {
		t.Fatalf("nextRecordingDownloadBackoff(2s) = %v, want %v", got, recordingDownloadMaxBackoff)
	}
}

func TestMatchingStoredRecordingNames(t *testing.T) {
	recordings := []ariStoredRecording{
		{Name: "other"},
		{Name: "matrixclaw_call_abc_20260617T083000Z"},
		{Name: "matrixclaw_call_abc_20260617T083100Z"},
	}
	got := matchingStoredRecordingNames(recordings, "matrixclaw_call_abc_20260617T083458Z", 1)
	want := []string{"matrixclaw_call_abc_20260617T083000Z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matchingStoredRecordingNames = %v, want %v", got, want)
	}
}
