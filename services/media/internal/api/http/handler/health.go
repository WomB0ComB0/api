package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"api/media/internal/domain/port"
)

type HealthStatus string

const (
	StatusHealthy   HealthStatus = "healthy"
	StatusUnhealthy HealthStatus = "unhealthy"
	StatusReady     HealthStatus = "ready"
	StatusNotReady  HealthStatus = "not_ready"
)

type HealthResponse struct {
	Status    HealthStatus `json:"status"`
	Timestamp time.Time    `json:"timestamp"`
	Service   string       `json:"service"`
}

type ReadinessChecks struct {
	Database HealthStatus `json:"database,omitempty"`
	R2       HealthStatus `json:"r2,omitempty"`
}

type ReadyResponse struct {
	Status    HealthStatus    `json:"status"`
	Timestamp time.Time       `json:"timestamp"`
	Service   string          `json:"service"`
	Checks    ReadinessChecks `json:"checks"`
}

func HealthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    StatusHealthy,
		Timestamp: time.Now().UTC(),
		Service:   "media",
	})
}

func ReadyCheck(store port.ObjectStore, repo port.AssetRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var (
			wg        sync.WaitGroup
			checks    ReadinessChecks
			r2Err     error
			dbErr     error
			hasStore  = store != nil
			hasRepo   = repo != nil
		)

		if hasStore {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r2Err = store.HealthCheck(ctx)
			}()
		}

		if hasRepo {
			wg.Add(1)
			go func() {
				defer wg.Done()
				dbErr = repo.HealthCheck(ctx)
			}()
		}

		wg.Wait()

		ready := true
		if hasStore {
			if r2Err != nil {
				checks.R2 = StatusUnhealthy
				ready = false
			} else {
				checks.R2 = StatusHealthy
			}
		}

		if hasRepo {
			if dbErr != nil {
				checks.Database = StatusUnhealthy
				ready = false
			} else {
				checks.Database = StatusHealthy
			}
		}

		status := http.StatusOK
		statusText := StatusReady
		if !ready {
			status = http.StatusServiceUnavailable
			statusText = StatusNotReady
		}

		writeJSON(w, status, ReadyResponse{
			Status:    statusText,
			Timestamp: time.Now().UTC(),
			Service:   "media",
			Checks:    checks,
		})
	}
}
