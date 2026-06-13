package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

func (s *Server) startCallRecording(ctx context.Context, call *Call, bridgeID string) *CallRecording {
	if s == nil || s.ari == nil || call == nil || !s.cfg.RecordCalls || strings.TrimSpace(bridgeID) == "" {
		return nil
	}
	format := normalizeRecordingFormat(s.cfg.RecordingFormat)
	captureFormat := recordingCaptureFormat(format)
	name := newRecordingName(call)
	recording := &CallRecording{
		Name:       name,
		Format:     format,
		MIMEType:   recordingMIMEType(format),
		Status:     "starting",
		Path:       recordingLocalPath(s.cfg.RecordingDir, name, format),
		Temporary:  false,
		StartedAt:  nil,
		FinishedAt: nil,
	}
	call.Recording = recording
	s.touchCall(call)

	live, err := s.ari.recordBridge(ctx, bridgeID, ariRecordRequest{
		Name:        name,
		Format:      captureFormat,
		IfExists:    "overwrite",
		TerminateOn: "none",
	})
	if err != nil {
		recording.Status = "failed"
		recording.Error = err.Error()
		s.touchCall(call)
		log.Printf("telephony call %s recording start failed: %v", callID(call), err)
		return recording
	}
	if live.Name != "" && live.Name != name {
		recording.Name = live.Name
		recording.Path = recordingLocalPath(s.cfg.RecordingDir, live.Name, format)
	}
	now := time.Now().UTC()
	recording.StartedAt = &now
	recording.Status = firstNonEmpty(live.State, "recording")
	s.touchCall(call)
	log.Printf("telephony call %s recording started: %s", callID(call), recording.Name)
	return recording
}

func (s *Server) finishCallRecording(parent context.Context, call *Call, recording *CallRecording) {
	if s == nil || s.ari == nil || call == nil || recording == nil || strings.TrimSpace(recording.Name) == "" {
		return
	}
	if strings.EqualFold(recording.Status, "failed") {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	recording.Status = "stopping"
	s.touchCall(call)
	if err := s.ari.stopLiveRecording(ctx, recording.Name); err != nil {
		recordingAddError(recording, "stop: "+err.Error())
		log.Printf("telephony call %s recording stop failed: %v", callID(call), err)
	}

	data, err := s.downloadRecordingWithRetry(ctx, recording.Name)
	if err != nil {
		recording.Status = "failed"
		recordingAddError(recording, "download: "+err.Error())
		finished := time.Now().UTC()
		recording.FinishedAt = &finished
		s.touchCall(call)
		log.Printf("telephony call %s recording download failed: %v", callID(call), err)
		return
	}
	if len(data) == 0 {
		recording.Status = "failed"
		recordingAddError(recording, "download: empty recording")
		finished := time.Now().UTC()
		recording.FinishedAt = &finished
		s.touchCall(call)
		return
	}
	recording.Status = "converting"
	s.touchCall(call)
	data, err = convertRecordingData(ctx, data, recordingCaptureFormat(recording.Format), recording.Format)
	if err != nil {
		recording.Status = "failed"
		recordingAddError(recording, "convert: "+err.Error())
		finished := time.Now().UTC()
		recording.FinishedAt = &finished
		s.touchCall(call)
		log.Printf("telephony call %s recording convert failed: %v", callID(call), err)
		return
	}
	recording.Size = int64(len(data))
	if err := writeRecordingFile(recording.Path, data); err != nil {
		recording.Status = "failed"
		recordingAddError(recording, "local save: "+err.Error())
		finished := time.Now().UTC()
		recording.FinishedAt = &finished
		s.touchCall(call)
		log.Printf("telephony call %s recording save failed: %v", callID(call), err)
		return
	}
	recording.Status = "saved"
	if s.cfg.RecordingStorage {
		if tempPath, err := s.saveRecordingTemporary(ctx, call, recording, data); err != nil {
			recordingAddError(recording, "temp storage: "+err.Error())
			log.Printf("telephony call %s recording temp storage save failed: %v", callID(call), err)
		} else {
			recording.Temporary = true
			recording.TempStoragePath = tempPath
		}
	}
	finished := time.Now().UTC()
	recording.FinishedAt = &finished
	s.touchCall(call)
	log.Printf("telephony call %s recording saved: %s", callID(call), recording.Path)
}

func (s *Server) downloadRecordingWithRetry(ctx context.Context, name string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		data, err := s.ari.downloadStoredRecording(ctx, name)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isARIStatus(err, http.StatusNotFound) && !isARIStatus(err, http.StatusConflict) {
			return nil, err
		}
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (s *Server) saveRecordingTemporary(ctx context.Context, call *Call, recording *CallRecording, data []byte) (string, error) {
	if s == nil || strings.TrimSpace(s.cfg.MatrixclawURL) == "" || strings.TrimSpace(s.cfg.MatrixclawToken) == "" {
		return "", fmt.Errorf("matrixclaw API is not configured")
	}
	tempPath := recordingStoragePath(s.cfg.RecordingPrefix, recording.Name, recording.Format)
	payload := map[string]any{
		"path":           tempPath,
		"content_base64": base64.StdEncoding.EncodeToString(data),
		"title":          recordingFileName(recording.Name, recording.Format),
		"tags":           []string{"telephony", "call-recording", strings.TrimSpace(call.Direction)},
		"mime_type":      recording.MIMEType,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.MatrixclawURL+"/v1/modules/storage/temp", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.MatrixclawToken)
	client := &http.Client{Timeout: 45 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var response struct {
		File struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"file"`
	}
	_ = json.NewDecoder(res.Body).Decode(&response)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if response.File.Size > 0 {
		recording.Size = response.File.Size
	}
	return firstNonEmpty(response.File.Path, tempPath), nil
}

func callRecordingSnapshot(call *Call) *CallRecording {
	if call == nil || call.Recording == nil {
		return nil
	}
	copy := *call.Recording
	return &copy
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
	if call != nil && strings.TrimSpace(call.ID) != "" {
		base = safeARIID(call.ID)
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
		return format
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
