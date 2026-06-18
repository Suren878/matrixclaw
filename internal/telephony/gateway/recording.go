package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CallRecording struct {
	Name            string     `json:"name,omitempty"`
	Format          string     `json:"format,omitempty"`
	MIMEType        string     `json:"mime_type,omitempty"`
	Status          string     `json:"status,omitempty"`
	Path            string     `json:"path,omitempty"`
	Temporary       bool       `json:"temporary,omitempty"`
	TempStoragePath string     `json:"temp_storage_path,omitempty"`
	Size            int64      `json:"size,omitempty"`
	Error           string     `json:"error,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

const (
	recordingFinishTimeout          = 45 * time.Second
	recordingDownloadMaxAttempts    = 14
	recordingDownloadInitialBackoff = 250 * time.Millisecond
	recordingDownloadMaxBackoff     = 2 * time.Second
	recordingStoredListTimeout      = 3 * time.Second
)

func (s *Server) startChannelRecording(ctx context.Context, call *Call, channelID string) *CallRecording {
	if s == nil || s.ari == nil || call == nil || !s.cfg.RecordCalls || strings.TrimSpace(channelID) == "" {
		return nil
	}
	format := normalizeRecordingFormat(s.cfg.RecordingFormat)
	captureFormat := recordingCaptureFormat(format)
	name := newRecordingName(call)
	recording := &CallRecording{
		Name:      name,
		Format:    format,
		MIMEType:  recordingMIMEType(format),
		Status:    "starting",
		Path:      recordingLocalPath(s.cfg.RecordingDir, name, format),
		Temporary: false,
	}
	s.setCallRecording(call, recording)

	live, err := s.ari.recordChannel(ctx, channelID, ariRecordRequest{
		Name:        name,
		Format:      captureFormat,
		IfExists:    "overwrite",
		TerminateOn: "none",
	})
	if err != nil {
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Status = "failed"
			r.Error = err.Error()
		})
		log.Printf("telephony call %s channel recording start failed: %v", callID(call), err)
		return recording
	}
	if live.Name != "" && live.Name != name {
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Name = live.Name
			r.Path = recordingLocalPath(s.cfg.RecordingDir, live.Name, format)
		})
	}
	now := time.Now().UTC()
	s.updateCallRecording(call, recording, func(r *CallRecording) {
		r.StartedAt = &now
		r.Status = firstNonEmpty(live.State, "recording")
	})
	recordingSnapshot, _ := callRecordingRefSnapshot(call, recording)
	log.Printf(
		"telephony call %s channel recording started: name=%s state=%s capture_format=%s output_format=%s target=%s",
		callID(call),
		recordingSnapshot.Name,
		firstNonEmpty(live.State, "recording"),
		firstNonEmpty(live.Format, captureFormat),
		recordingSnapshot.Format,
		strings.TrimSpace(live.TargetURI),
	)
	return recording
}

func (s *Server) finishCallRecording(parent context.Context, call *Call, recording *CallRecording) {
	recordingSnapshot, ok := callRecordingRefSnapshot(call, recording)
	if s == nil || s.ari == nil || call == nil || recording == nil || !ok || strings.TrimSpace(recordingSnapshot.Name) == "" {
		return
	}
	if strings.EqualFold(recordingSnapshot.Status, "failed") {
		return
	}
	ctx, cancel := context.WithTimeout(parent, recordingFinishTimeout)
	defer cancel()

	s.updateCallRecording(call, recording, func(r *CallRecording) {
		r.Status = "stopping"
	})
	var stopErr error
	if err := s.ari.stopLiveRecording(ctx, recordingSnapshot.Name); err != nil {
		stopErr = err
		log.Printf("telephony call %s recording stop failed: %v", callID(call), err)
	}
	addStopError := func(r *CallRecording) {
		if stopErr != nil {
			recordingAddError(r, "stop: "+stopErr.Error())
		}
	}

	data, err := s.downloadRecordingWithRetry(ctx, recordingSnapshot.Name)
	if err != nil {
		finished := time.Now().UTC()
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Status = "failed"
			addStopError(r)
			recordingAddError(r, "download: "+err.Error())
			r.FinishedAt = &finished
		})
		log.Printf("telephony call %s recording download failed: %v", callID(call), err)
		return
	}
	if len(data) == 0 {
		finished := time.Now().UTC()
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Status = "failed"
			addStopError(r)
			recordingAddError(r, "download: empty recording")
			r.FinishedAt = &finished
		})
		return
	}
	s.updateCallRecording(call, recording, func(r *CallRecording) {
		r.Status = "converting"
	})
	recordingSnapshot, _ = callRecordingRefSnapshot(call, recording)
	data, err = convertRecordingData(ctx, data, recordingCaptureFormat(recordingSnapshot.Format), recordingSnapshot.Format)
	if err != nil {
		finished := time.Now().UTC()
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Status = "failed"
			addStopError(r)
			recordingAddError(r, "convert: "+err.Error())
			r.FinishedAt = &finished
		})
		log.Printf("telephony call %s recording convert failed: %v", callID(call), err)
		return
	}
	s.updateCallRecording(call, recording, func(r *CallRecording) {
		r.Size = int64(len(data))
	})
	recordingSnapshot, _ = callRecordingRefSnapshot(call, recording)
	if err := writeRecordingFile(recordingSnapshot.Path, data); err != nil {
		finished := time.Now().UTC()
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Status = "failed"
			addStopError(r)
			recordingAddError(r, "local save: "+err.Error())
			r.FinishedAt = &finished
		})
		log.Printf("telephony call %s recording save failed: %v", callID(call), err)
		return
	}
	s.updateCallRecording(call, recording, func(r *CallRecording) {
		r.Status = "saved"
	})
	if s.cfg.RecordingStorage {
		if tempPath, err := s.saveRecordingTemporary(ctx, call, recording, data); err != nil {
			s.updateCallRecording(call, recording, func(r *CallRecording) {
				recordingAddError(r, "temp storage: "+err.Error())
			})
			log.Printf("telephony call %s recording temp storage save failed: %v", callID(call), err)
		} else {
			s.updateCallRecording(call, recording, func(r *CallRecording) {
				r.Temporary = true
				r.TempStoragePath = tempPath
			})
		}
	}
	finished := time.Now().UTC()
	s.updateCallRecording(call, recording, func(r *CallRecording) {
		r.FinishedAt = &finished
	})
	recordingSnapshot, _ = callRecordingRefSnapshot(call, recording)
	log.Printf("telephony call %s recording saved: %s", callID(call), recordingSnapshot.Path)
}

func (s *Server) downloadRecordingWithRetry(ctx context.Context, name string) ([]byte, error) {
	if s == nil || s.ari == nil {
		return nil, fmt.Errorf("ARI client is not configured")
	}
	data, err := downloadRecordingWithBackoff(ctx, name, s.ari.downloadStoredRecording, sleepContext)
	if err != nil && isRecordingDownloadRetryable(err) {
		s.logStoredRecordingCandidates(ctx, name)
	}
	return data, err
}

type recordingDownloadFunc func(context.Context, string) ([]byte, error)
type recordingSleepFunc func(context.Context, time.Duration) bool

func downloadRecordingWithBackoff(ctx context.Context, name string, download recordingDownloadFunc, sleep recordingSleepFunc) ([]byte, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("recording name is empty")
	}
	if download == nil {
		return nil, fmt.Errorf("recording downloader is not configured")
	}
	if sleep == nil {
		sleep = sleepContext
	}
	var lastErr error
	delay := recordingDownloadInitialBackoff
	for attempt := 0; attempt < recordingDownloadMaxAttempts; attempt++ {
		data, err := download(ctx, name)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isRecordingDownloadRetryable(err) {
			return nil, err
		}
		if attempt == recordingDownloadMaxAttempts-1 {
			break
		}
		if !sleep(ctx, delay) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, lastErr
		}
		delay = nextRecordingDownloadBackoff(delay)
	}
	return nil, fmt.Errorf("recording %q was not available after %d attempts: %w", name, recordingDownloadMaxAttempts, lastErr)
}

func nextRecordingDownloadBackoff(delay time.Duration) time.Duration {
	if delay <= 0 {
		return recordingDownloadInitialBackoff
	}
	next := delay * 2
	if next > recordingDownloadMaxBackoff {
		return recordingDownloadMaxBackoff
	}
	return next
}

func isRecordingDownloadRetryable(err error) bool {
	return isARIStatus(err, http.StatusNotFound) || isARIStatus(err, http.StatusConflict)
}

func (s *Server) logStoredRecordingCandidates(parent context.Context, name string) {
	if s == nil || s.ari == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, recordingStoredListTimeout)
	defer cancel()
	recordings, err := s.ari.listStoredRecordings(ctx)
	if err != nil {
		log.Printf("telephony recording stored list failed for %s: %v", strings.TrimSpace(name), err)
		return
	}
	matches := matchingStoredRecordingNames(recordings, name, 5)
	if len(matches) == 0 {
		log.Printf("telephony recording stored list has no candidates for %s", strings.TrimSpace(name))
		return
	}
	log.Printf("telephony recording stored candidates for %s: %s", strings.TrimSpace(name), strings.Join(matches, ","))
}

func matchingStoredRecordingNames(recordings []ariStoredRecording, name string, limit int) []string {
	name = strings.TrimSpace(name)
	if name == "" || limit <= 0 {
		return nil
	}
	prefix := name
	if idx := strings.LastIndex(name, "_"); idx > 0 {
		prefix = name[:idx]
	}
	var matches []string
	for _, recording := range recordings {
		candidate := strings.TrimSpace(recording.Name)
		if candidate == "" {
			continue
		}
		if candidate == name || strings.HasPrefix(candidate, prefix) {
			matches = append(matches, candidate)
			if len(matches) >= limit {
				return matches
			}
		}
	}
	return matches
}

func (s *Server) saveRecordingTemporary(ctx context.Context, call *Call, recording *CallRecording, data []byte) (string, error) {
	if s == nil || strings.TrimSpace(s.cfg.MatrixclawURL) == "" || strings.TrimSpace(s.cfg.MatrixclawToken) == "" {
		return "", fmt.Errorf("matrixclaw API is not configured")
	}
	recordingSnapshot, ok := callRecordingRefSnapshot(call, recording)
	if !ok {
		return "", fmt.Errorf("recording is not attached to call")
	}
	snapshot := callSnapshot(call)
	tempPath := recordingStoragePath(s.cfg.RecordingPrefix, recordingSnapshot.Name, recordingSnapshot.Format)
	payload := map[string]any{
		"path":           tempPath,
		"content_base64": base64.StdEncoding.EncodeToString(data),
		"title":          recordingFileName(recordingSnapshot.Name, recordingSnapshot.Format),
		"tags":           []string{"telephony", "call-recording", strings.TrimSpace(snapshot.Direction)},
		"mime_type":      recordingSnapshot.MIMEType,
	}
	var response struct {
		File struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"file"`
	}
	if err := s.api.postJSON(ctx, "/v1/modules/storage/temp", payload, &response); err != nil {
		return "", err
	}
	if response.File.Size > 0 {
		s.updateCallRecording(call, recording, func(r *CallRecording) {
			r.Size = response.File.Size
		})
	}
	return firstNonEmpty(response.File.Path, tempPath), nil
}

func formatCallRecording(recording CallRecording) string {
	var lines []string
	if recording.Status != "" {
		lines = append(lines, "status: "+strings.TrimSpace(recording.Status))
	}
	if recording.TempStoragePath != "" {
		lines = append(lines, "temp_storage_path: "+strings.TrimSpace(recording.TempStoragePath))
		lines = append(lines, "temporary: true")
	}
	if recording.Path != "" {
		lines = append(lines, "local_path: "+strings.TrimSpace(recording.Path))
	}
	if recording.MIMEType != "" {
		lines = append(lines, "mime_type: "+strings.TrimSpace(recording.MIMEType))
	}
	if recording.Size > 0 {
		lines = append(lines, fmt.Sprintf("size_bytes: %d", recording.Size))
	}
	if recording.Error != "" {
		lines = append(lines, "error: "+strings.TrimSpace(recording.Error))
	}
	return strings.Join(lines, "\n")
}

func newRecordingName(call *Call) string {
	base := "call"
	if id := callID(call); id != "" {
		base = safeARIID(id)
	}
	return "matrixclaw_" + base + "_" + time.Now().UTC().Format("20060102T150405Z")
}

func recordingLocalPath(dir string, name string, format string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = defaultRecordingDir()
	}
	return filepath.Join(dir, recordingFileName(name, format))
}

func recordingStoragePath(prefix string, name string, format string) string {
	prefix = normalizeRecordingPrefix(prefix)
	return prefix + "/" + recordingFileName(name, format)
}

func recordingFileName(name string, format string) string {
	name = recordingPathSegment(name)
	if name == "" {
		name = "call-recording"
	}
	ext := recordingFileExtension(format)
	return name + "." + ext
}

func recordingFileExtension(format string) string {
	format = normalizeRecordingFormat(format)
	switch format {
	case "mp3", "wav", "gsm", "ulaw", "alaw", "sln":
		return format
	default:
		return defaultRecordingFormat
	}
}

func recordingMIMEType(format string) string {
	switch normalizeRecordingFormat(format) {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "gsm":
		return "audio/gsm"
	case "ulaw", "alaw", "sln":
		return "audio/basic"
	default:
		return "application/octet-stream"
	}
}

func recordingPathSegment(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}

func recordingAddError(recording *CallRecording, text string) {
	if recording == nil || strings.TrimSpace(text) == "" {
		return
	}
	if strings.TrimSpace(recording.Error) == "" {
		recording.Error = strings.TrimSpace(text)
		return
	}
	recording.Error += "; " + strings.TrimSpace(text)
}

func writeRecordingFile(path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("recording path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func recordingCaptureFormat(outputFormat string) string {
	if strings.EqualFold(normalizeRecordingFormat(outputFormat), "mp3") {
		return "wav"
	}
	return normalizeRecordingFormat(outputFormat)
}

func convertRecordingData(ctx context.Context, data []byte, sourceFormat string, targetFormat string) ([]byte, error) {
	sourceFormat = normalizeRecordingFormat(sourceFormat)
	targetFormat = normalizeRecordingFormat(targetFormat)
	if targetFormat == "" || strings.EqualFold(sourceFormat, targetFormat) {
		return data, nil
	}
	switch targetFormat {
	case "mp3":
		return convertRecordingToMP3(ctx, data)
	default:
		return nil, fmt.Errorf("unsupported recording output format %q", targetFormat)
	}
}

func convertRecordingToMP3(ctx context.Context, data []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-codec:a", "libmp3lame",
		"-b:a", "64k",
		"-ar", "16000",
		"-ac", "1",
		"-f", "mp3",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty mp3")
	}
	return out.Bytes(), nil
}
