package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

const (
	defaultJSONBodyLimitBytes     int64 = 1 << 20
	storageMaxContentBytes        int64 = 25 << 20
	storageMaxBase64ContentBytes  int64 = ((storageMaxContentBytes + 2) / 3) * 4
	storageReadLimitBytes         int64 = storageMaxContentBytes
	storageSaveJSONBodyLimitBytes int64 = 36 << 20
	voiceAudioJSONBodyLimitBytes  int64 = 36 << 20
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErrorMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, core.ErrorResponse{Error: message})
}

func writeInvalidJSON(w http.ResponseWriter) {
	writeErrorMessage(w, http.StatusBadRequest, "invalid json body")
}

func writeNotFound(w http.ResponseWriter) {
	writeErrorMessage(w, http.StatusNotFound, "not found")
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONBodyLimit(w, r, target, defaultJSONBodyLimitBytes)
}

func decodeJSONBodyLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	if requestBodyTooLarge(r, limit) {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	decoder := newLimitedJSONDecoder(w, r, limit)
	if err := decoder.Decode(target); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidJSON(w)
		return false
	}
	return true
}

func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeOptionalJSONBodyLimit(w, r, target, defaultJSONBodyLimitBytes)
}

func decodeOptionalJSONBodyLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	if requestBodyTooLarge(r, limit) {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	decoder := newLimitedJSONDecoder(w, r, limit)
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		writeJSONDecodeError(w, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidJSON(w)
		return false
	}
	return true
}

func newLimitedJSONDecoder(w http.ResponseWriter, r *http.Request, limit int64) *json.Decoder {
	if limit <= 0 {
		limit = defaultJSONBodyLimitBytes
	}
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
}

func requestBodyTooLarge(r *http.Request, limit int64) bool {
	return limit > 0 && r.ContentLength > limit
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeInvalidJSON(w)
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", joinAllowed(allowed))
	writeErrorMessage(w, http.StatusMethodNotAllowed, "method not allowed")
}

func joinAllowed(allowed []string) string {
	if len(allowed) == 0 {
		return ""
	}

	result := allowed[0]
	for i := 1; i < len(allowed); i++ {
		result += ", " + allowed[i]
	}
	return result
}

func writeError(w http.ResponseWriter, err error) {
	writeErrorMessage(w, statusForCoreError(err), err.Error())
}

func writeAcceptRunError(w http.ResponseWriter, result core.AcceptRunResult, err error) {
	if result.Run.ID == "" {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusInternalServerError, core.AcceptRunErrorResponse{
		Error:       err.Error(),
		SessionID:   result.SessionID,
		UserMessage: result.UserMessage,
		Run:         result.Run,
	})
}

func statusForCoreError(err error) int {
	switch {
	case errors.Is(err, core.ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, core.ErrBindingNotFound), errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, core.ErrSessionSelectionRequired):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
