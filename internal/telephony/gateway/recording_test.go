package gateway

import "testing"

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
