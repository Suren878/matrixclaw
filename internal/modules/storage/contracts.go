package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type FileSaveRequest struct {
	Path          string   `json:"path"`
	ContentBase64 string   `json:"content_base64"`
	Title         string   `json:"title"`
	Tags          []string `json:"tags"`
	MIMEType      string   `json:"mime_type"`
}

func (r *FileSaveRequest) UnmarshalJSON(data []byte) error {
	type fileSaveRequest FileSaveRequest

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, ok := fields["content"]; ok {
		return errors.New("content is not supported; use content_base64")
	}
	if _, ok := fields["content_base64"]; !ok {
		return errors.New("content_base64 is required")
	}

	var decoded fileSaveRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = FileSaveRequest(decoded)
	return nil
}

func NewFileSaveRequest(path string, content []byte, title string, tags []string, mimeType string) FileSaveRequest {
	return FileSaveRequest{
		Path:          strings.TrimSpace(path),
		ContentBase64: base64.StdEncoding.EncodeToString(content),
		Title:         strings.TrimSpace(title),
		Tags:          tags,
		MIMEType:      strings.TrimSpace(mimeType),
	}
}

func (r FileSaveRequest) ContentBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(r.ContentBase64))
}

type FileResponse struct {
	File Entry `json:"file"`
}

type ReadBytesResponse struct {
	File          Entry  `json:"file"`
	ContentBase64 string `json:"content_base64"`
}

func NewReadBytesResponse(file Entry, content []byte) ReadBytesResponse {
	return ReadBytesResponse{File: file, ContentBase64: base64.StdEncoding.EncodeToString(content)}
}

func (r ReadBytesResponse) ContentBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(r.ContentBase64))
}

type TempFileResponse struct {
	File TempEntry `json:"file"`
}

type TempReadBytesResponse struct {
	File          TempEntry `json:"file"`
	ContentBase64 string    `json:"content_base64"`
}

func NewTempReadBytesResponse(file TempEntry, content []byte) TempReadBytesResponse {
	return TempReadBytesResponse{File: file, ContentBase64: base64.StdEncoding.EncodeToString(content)}
}

func (r TempReadBytesResponse) ContentBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(r.ContentBase64))
}

type TempPromoteRequest struct {
	DestPath string `json:"dest_path"`
}

type TempSettingsUpdateRequest struct {
	AutoCleanup *bool   `json:"auto_cleanup,omitempty"`
	TTLDays     int64   `json:"ttl_days"`
	MaxGB       float64 `json:"max_gb"`
}

type TempSettingsResponse struct {
	Settings TempSettings `json:"settings"`
}

type CleanupResponse struct {
	Cleanup CleanupResult `json:"cleanup"`
}
