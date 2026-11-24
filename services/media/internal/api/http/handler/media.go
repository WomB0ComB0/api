package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"api/media/internal/api/http/middleware"
	"api/media/internal/application"
	"api/media/internal/application/media"
	"api/media/internal/pkg/apperr"

	"github.com/go-chi/chi/v5"
)

const multipartMemoryLimit = 32 << 20 // 32MB in memory, rest spooled to disk

// MediaHandler is a thin primary HTTP adapter delegating all business logic to application use cases.
type MediaHandler struct {
	factory        *application.Factory
	maxUploadBytes int64
}

func NewMediaHandler(factory *application.Factory, maxUploadBytes int64) *MediaHandler {
	if maxUploadBytes <= 0 {
		maxUploadBytes = 100 << 20
	}
	return &MediaHandler{
		factory:        factory,
		maxUploadBytes: maxUploadBytes,
	}
}

func actorFromRequest(r *http.Request) media.Actor {
	return media.Actor{
		UserID: middleware.GetUserID(r.Context()),
		Role:   middleware.GetUserRole(r.Context()),
	}
}

func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(multipartMemoryLimit); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeAppError(w, apperr.TooLarge("file exceeds maximum allowed size"))
			return
		}
		writeAppError(w, apperr.Validation("invalid multipart form"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeAppError(w, apperr.Validation("missing 'file' form field"))
		return
	}
	defer file.Close()

	out, err := h.factory.Upload().Execute(r.Context(), media.UploadInput{
		Actor:             actorFromRequest(r),
		Filename:          header.Filename,
		HeaderContentType: header.Header.Get("Content-Type"),
		Visibility:        r.FormValue("visibility"),
		File:              file,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *MediaHandler) PresignUpload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Visibility  string `json:"visibility"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeAppError(w, err)
		return
	}

	out, err := h.factory.PresignUpload().Execute(r.Context(), media.PresignInput{
		Actor:       actorFromRequest(r),
		Filename:    body.Filename,
		ContentType: body.ContentType,
		Visibility:  body.Visibility,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MediaHandler) ConfirmUpload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key      string         `json:"key"`
		Filename string         `json:"filename"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeAppError(w, err)
		return
	}

	out, err := h.factory.ConfirmUpload().Execute(r.Context(), media.ConfirmUploadInput{
		Actor:    actorFromRequest(r),
		Key:      body.Key,
		Filename: body.Filename,
		Metadata: body.Metadata,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *MediaHandler) SignURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AssetID   string `json:"asset_id"`
		Key       string `json:"key"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeAppError(w, err)
		return
	}

	out, err := h.factory.SignURL().Execute(r.Context(), media.SignURLInput{
		Actor:     actorFromRequest(r),
		AssetID:   body.AssetID,
		Key:       body.Key,
		ExpiresIn: body.ExpiresIn,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MediaHandler) PurgeCache(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Files []string `json:"files"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeAppError(w, err)
		return
	}

	out, err := h.factory.PurgeCache().Execute(r.Context(), media.PurgeCacheInput{
		Actor: actorFromRequest(r),
		Files: body.Files,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MediaHandler) ListAssets(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)

	out, err := h.factory.ListAssets().Execute(r.Context(), media.ListAssetsInput{
		Actor:  actorFromRequest(r),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MediaHandler) GetAsset(w http.ResponseWriter, r *http.Request) {
	out, err := h.factory.GetAsset().Execute(r.Context(), media.GetAssetInput{
		Actor:   actorFromRequest(r),
		AssetID: chi.URLParam(r, "id"),
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MediaHandler) DeleteAsset(w http.ResponseWriter, r *http.Request) {
	if err := h.factory.DeleteAsset().Execute(r.Context(), media.DeleteAssetInput{
		Actor:   actorFromRequest(r),
		AssetID: chi.URLParam(r, "id"),
	}); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperr.Validation("invalid JSON payload")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return apperr.Validation("unexpected trailing data in JSON payload")
	}
	return nil
}

func parseIntQuery(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
