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

const defaultReadyCacheTTL = 2 * time.Second

func ReadyCheck(store port.ObjectStore, repo port.AssetRepository) http.HandlerFunc {
	return ReadyCheckWithTTL(store, repo, defaultReadyCacheTTL)
}

// ReadyCheckWithTTL coalesces readiness probes (Postgres Ping + R2 HeadBucket) over ttl
// so floods against the rate-limit-exempt /health/ready endpoint cannot exhaust DB pools or R2 quotas.
func ReadyCheckWithTTL(store port.ObjectStore, repo port.AssetRepository, ttl time.Duration) http.HandlerFunc {
	type cachedReady struct {
		status    int
		payload   ReadyResponse
		expiresAt time.Time
	}

	var (
		mu       sync.Mutex
		cached   *cachedReady
		inflight chan struct{}
	)

	probe := func() (int, ReadyResponse) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var (
			wg       sync.WaitGroup
			checks   ReadinessChecks
			r2Err    error
			dbErr    error
			hasStore = store != nil
			hasRepo  = repo != nil
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

		return status, ReadyResponse{
			Status:    statusText,
			Timestamp: time.Now().UTC(),
			Service:   "media",
			Checks:    checks,
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if ttl <= 0 {
			status, payload := probe()
			writeJSON(w, status, payload)
			return
		}

		for {
			now := time.Now()
			mu.Lock()
			if cached != nil && now.Before(cached.expiresAt) {
				res := *cached
				mu.Unlock()
				writeJSON(w, res.status, res.payload)
				return
			}
			if inflight != nil {
				waitCh := inflight
				mu.Unlock()
				select {
				case <-waitCh:
					continue
				case <-r.Context().Done():
					writeJSON(w, http.StatusServiceUnavailable, ReadyResponse{
						Status:    StatusNotReady,
						Timestamp: time.Now().UTC(),
						Service:   "media",
					})
					return
				}
			}
			doneCh := make(chan struct{})
			inflight = doneCh
			mu.Unlock()

			status, payload := probe()

			mu.Lock()
			cached = &cachedReady{
				status:    status,
				payload:   payload,
				expiresAt: time.Now().Add(ttl),
			}
			inflight = nil
			close(doneCh)
			mu.Unlock()

			writeJSON(w, status, payload)
			return
		}
	}
}
