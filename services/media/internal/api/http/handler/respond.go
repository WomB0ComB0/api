package handler

import (
	"bytes"
	"encoding/json"
	"net/http"

	"api/media/internal/pkg/apperr"

	"github.com/rs/zerolog/log"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		http.Error(w, `{"error":"encoding error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeAppError(w http.ResponseWriter, err error) {
	if appErr, ok := apperr.As(err); ok {
		status := appErr.HTTPStatus()
		if status >= 500 {
			log.Error().Err(appErr.Cause).Str("type", string(appErr.Type)).Msg(appErr.Message)
		}
		payload := map[string]any{
			"error": appErr.Message,
			"code":  appErr.Type,
		}
		if len(appErr.Details) > 0 {
			payload["details"] = appErr.Details
		}
		writeJSON(w, status, payload)
		return
	}

	log.Error().Err(err).Msg("Unhandled internal error")
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": "Internal server error",
		"code":  apperr.TypeSystem,
	})
}
