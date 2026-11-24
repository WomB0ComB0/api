package handler

import (
	"bytes"
	"encoding/json"
	"net/http"

	"api/media/internal/pkg/apperr"

	"github.com/rs/zerolog/log"
)

type errorResponse struct {
	Error   string           `json:"error"`
	Code    apperr.ErrorType `json:"code"`
	Details map[string]any   `json:"details,omitempty"`
}

func writeJSON[T any](w http.ResponseWriter, status int, payload T) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		http.Error(w, `{"error":"encoding error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeAppError(w http.ResponseWriter, err error) {
	if appErr, ok := apperr.As(err); ok {
		status := appErr.HTTPStatus()
		if status >= 500 {
			log.Error().Err(appErr.Cause).Str("type", string(appErr.Type)).Msg(appErr.Message)
		}
		writeJSON(w, status, errorResponse{
			Error:   appErr.Message,
			Code:    appErr.Type,
			Details: appErr.Details,
		})
		return
	}

	log.Error().Err(err).Msg("Unhandled internal error")
	writeJSON(w, http.StatusInternalServerError, errorResponse{
		Error: "Internal server error",
		Code:  apperr.TypeSystem,
	})
}
