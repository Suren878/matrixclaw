package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

type storageStore interface {
	Root() string
	List(prefix string, query string, limit int) ([]localstorage.Entry, error)
	SaveBytes(rawPath string, content []byte, title string, tags []string, mimeType string) (localstorage.Entry, error)
	Read(rawPath string, maxBytes int64) (localstorage.Entry, string, error)
	Delete(rawPath string) (localstorage.Entry, error)
	ListTemporary(limit int) (localstorage.TempListResult, error)
	SaveTemporary(rawPath string, content []byte, title string, tags []string, mimeType string) (localstorage.TempEntry, error)
	CleanupTemporary() (localstorage.CleanupResult, error)
	UpdateTemporarySettings(autoCleanup *bool, ttlDays int64, maxGB float64) (localstorage.TempSettings, error)
	PromoteTemporary(rawPath string, destPath string) (localstorage.Entry, error)
	DeleteTemporary(rawPath string) (localstorage.TempEntry, error)
}

func (s *Server) handleStorageFiles(w http.ResponseWriter, r *http.Request) {
	if s.storageUnavailable(w) {
		return
	}
	if r.Method != http.MethodGet {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
		s.handleStorageFileCreate(w, r)
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	entries, err := s.storage.List(r.URL.Query().Get("prefix"), r.URL.Query().Get("query"), limit)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, localstorage.ListResult{
		Root:  s.storage.Root(),
		Files: entries,
	})
}

func (s *Server) handleStorageFileCreate(w http.ResponseWriter, r *http.Request) {
	var req localstorage.FileSaveRequest
	if !decodeJSONBodyLimit(w, r, &req, storageSaveJSONBodyLimitBytes) {
		return
	}
	content, ok := storageSaveContent(w, req)
	if !ok {
		return
	}
	entry, err := s.storage.SaveBytes(req.Path, content, req.Title, req.Tags, req.MIMEType)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, localstorage.FileResponse{File: entry})
}

func (s *Server) handleStorageFileByPath(w http.ResponseWriter, r *http.Request) {
	if s.storageUnavailable(w) {
		return
	}
	storagePath := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/modules/storage/files/"))
	if storagePath == "" {
		writeNotFound(w)
		return
	}
	if decoded, err := url.PathUnescape(storagePath); err == nil {
		storagePath = decoded
	}

	switch r.Method {
	case http.MethodGet:
		entry, content, err := s.storage.Read(storagePath, storageReadLimitBytes)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, localstorage.ReadResult{File: entry, Content: content})
	case http.MethodDelete:
		entry, err := s.storage.Delete(storagePath)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, localstorage.FileResponse{File: entry})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) handleStorageTemp(w http.ResponseWriter, r *http.Request) {
	if s.storageUnavailable(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		result, err := s.storage.ListTemporary(limit)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case http.MethodPost:
		s.handleStorageTempCreate(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleStorageTempCreate(w http.ResponseWriter, r *http.Request) {
	var req localstorage.FileSaveRequest
	if !decodeJSONBodyLimit(w, r, &req, storageSaveJSONBodyLimitBytes) {
		return
	}
	content, ok := storageSaveContent(w, req)
	if !ok {
		return
	}
	entry, err := s.storage.SaveTemporary(req.Path, content, req.Title, req.Tags, req.MIMEType)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, localstorage.TempFileResponse{File: entry})
}

func (s *Server) handleStorageTempByPath(w http.ResponseWriter, r *http.Request) {
	if s.storageUnavailable(w) {
		return
	}
	tempPath := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/modules/storage/temp/"))
	if tempPath == "" {
		writeNotFound(w)
		return
	}
	if tempPath == "cleanup" {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		result, err := s.storage.CleanupTemporary()
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, localstorage.CleanupResponse{Cleanup: result})
		return
	}
	if tempPath == "settings" {
		if r.Method != http.MethodPatch {
			writeMethodNotAllowed(w, http.MethodPatch)
			return
		}
		var req localstorage.TempSettingsUpdateRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		settings, err := s.storage.UpdateTemporarySettings(req.AutoCleanup, req.TTLDays, req.MaxGB)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, localstorage.TempSettingsResponse{Settings: settings})
		return
	}
	promote := strings.HasSuffix(tempPath, "/promote")
	if promote {
		tempPath = strings.TrimSuffix(tempPath, "/promote")
	}
	if decoded, err := url.PathUnescape(tempPath); err == nil {
		tempPath = decoded
	}
	switch {
	case promote && r.Method == http.MethodPost:
		var req localstorage.TempPromoteRequest
		if !decodeOptionalJSONBody(w, r, &req) {
			return
		}
		entry, err := s.storage.PromoteTemporary(tempPath, req.DestPath)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, localstorage.FileResponse{File: entry})
	case !promote && r.Method == http.MethodDelete:
		entry, err := s.storage.DeleteTemporary(tempPath)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, localstorage.TempFileResponse{File: entry})
	default:
		writeMethodNotAllowed(w, http.MethodDelete, http.MethodPost)
	}
}

func storageSaveContent(w http.ResponseWriter, req localstorage.FileSaveRequest) ([]byte, bool) {
	encoded := strings.TrimSpace(req.ContentBase64)
	if int64(len(encoded)) > storageMaxBase64ContentBytes {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	content, err := req.ContentBytes()
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "content_base64 is invalid")
		return nil, false
	}
	if int64(len(content)) > storageMaxContentBytes {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	return content, true
}

func (s *Server) storageUnavailable(w http.ResponseWriter) bool {
	if s.storage != nil {
		return false
	}
	writeErrorMessage(w, http.StatusNotFound, "storage module is not enabled")
	return true
}

func writeStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, localstorage.ErrInvalidPath):
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
	case strings.Contains(err.Error(), "not found"):
		writeErrorMessage(w, http.StatusNotFound, err.Error())
	default:
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
	}
}
