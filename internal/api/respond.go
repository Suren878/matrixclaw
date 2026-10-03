package api

import (
	"context"
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

type bodyLimitKey struct{}

// withBodyLimit raises the JSON body limit of one route.
func withBodyLimit(limit int64, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r.WithContext(context.WithValue(r.Context(), bodyLimitKey{}, limit)))
	}
}

// decodeJSON reads the request body into target; an empty body leaves target
// as it is. It answers 400 or 413 and returns false when the body is bad.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	limit := defaultJSONBodyLimitBytes
	if routeLimit, ok := r.Context().Value(bodyLimitKey{}).(int64); ok {
		limit = routeLimit
	}
	if r.ContentLength > limit {
		writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeErrorMessage(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeInvalidJSON(w)
		return false
	} else if err == nil {
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeInvalidJSON(w)
			return false
		}
	}
	return true
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
	case errors.Is(err, core.ErrRunActive):
		return http.StatusConflict
	case errors.Is(err, core.ErrSessionRestricted), errors.Is(err, core.ErrOwnerOnly):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
