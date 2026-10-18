package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/skills"
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

// queryInt reads an integer query parameter, fallback when absent; it
// answers 400 and returns false for one that is not a number.
func queryInt(w http.ResponseWriter, r *http.Request, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return value, true
}

// errBodyTooLarge reports a request body over the route's limit.
var errBodyTooLarge = errors.New("request body too large")

// errorStatuses maps sentinel errors to the status they answer; the first
// match wins.
var errorStatuses = []struct {
	err    error
	status int
}{
	{core.ErrInvalidInput, http.StatusBadRequest},
	{core.ErrNotFound, http.StatusNotFound},
	{core.ErrBindingNotFound, http.StatusNotFound},
	{core.ErrSessionSelectionRequired, http.StatusConflict},
	{core.ErrRunActive, http.StatusConflict},
	{core.ErrSessionRestricted, http.StatusForbidden},
	{core.ErrOwnerOnly, http.StatusForbidden},
	{core.ErrExecutionUnavailable, http.StatusServiceUnavailable},
	{errBodyTooLarge, http.StatusRequestEntityTooLarge},
	{localstorage.ErrInvalidPath, http.StatusBadRequest},
	{localstorage.ErrNotFound, http.StatusNotFound},
	{skills.ErrNotFound, http.StatusNotFound},
	{modules.ErrUnknownSetting, http.StatusNotFound},
	{modules.ErrInvalidSetting, http.StatusBadRequest},
	{voicemodule.ErrInvalidRequest, http.StatusBadRequest},
	{voicemodule.ErrUnsupportedProvider, http.StatusBadRequest},
	{voicemodule.ErrModuleDisabled, http.StatusConflict},
	{voicemodule.ErrProviderUnavailable, http.StatusConflict},
	{realtime.ErrInvalidRequest, http.StatusBadRequest},
	{realtime.ErrSessionNotFound, http.StatusNotFound},
	{realtime.ErrDisabled, http.StatusConflict},
	{realtime.ErrProviderUnavailable, http.StatusConflict},
}

// statusError carries the status to answer when no sentinel matches.
type statusError struct {
	status int
	err    error
}

func (e statusError) Error() string { return e.err.Error() }
func (e statusError) Unwrap() error { return e.err }

// withStatus answers err with status unless a sentinel in it says otherwise.
func withStatus(status int, err error) error {
	return statusError{status: status, err: err}
}

// writeError is the one error answer: the status of the first sentinel err
// wraps, else the one withStatus gave it, else 500.
func writeError(w http.ResponseWriter, err error) {
	writeErrorMessage(w, statusOf(err), err.Error())
}

func statusOf(err error) int {
	for _, entry := range errorStatuses {
		if errors.Is(err, entry.err) {
			return entry.status
		}
	}
	var carried statusError
	if errors.As(err, &carried) {
		return carried.status
	}
	return http.StatusInternalServerError
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
