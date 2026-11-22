package handler

import (
	"context"
	"net/http"
	"time"

	"api/media/internal/domain/port"
)

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Service   string    `json:"service"`
}

type ReadyResponse struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Service   string            `json:"service"`
	Checks    map[string]string `json:"checks"`
}

func HealthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC(),
		Service:   "media",
	})
}

func ReadyCheck(store port.ObjectStore, repo port.AssetRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		checks := map[string]string{}
		ready := true

		if store != nil {
			if err := store.HealthCheck(ctx); err != nil {
				checks["r2"] = "unhealthy"
				ready = false
			} else {
				checks["r2"] = "healthy"
			}
		}

		if repo != nil {
			if err := repo.HealthCheck(ctx); err != nil {
				checks["database"] = "unhealthy"
				ready = false
			} else {
				checks["database"] = "healthy"
			}
		}

		status := http.StatusOK
		statusText := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			statusText = "not_ready"
		}

		writeJSON(w, status, ReadyResponse{
			Status:    statusText,
			Timestamp: time.Now().UTC(),
			Service:   "media",
			Checks:    checks,
		})
	}
}
