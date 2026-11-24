package middleware

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitPolicy defines a sliding-window counter policy for a route class.
type RateLimitPolicy struct {
	Name   string
	Limit  int
	Window time.Duration
}

type slidingBucket struct {
	windowStart int64
	prevCount   int
	currCount   int
	lastSeen    int64
}

// SlidingWindowLimiter implements an O(1)-per-key weighted sliding-window counter
// with a bounded key map (prevents memory exhaustion under IP floods) and emits
// standard IETF RateLimit-* + RateLimit-Policy + Retry-After headers.
type SlidingWindowLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*slidingBucket
	maxBuckets int
	now        func() time.Time
}

// NewSlidingWindowLimiter creates a bounded weighted sliding-window rate limiter.
func NewSlidingWindowLimiter(maxBuckets int) *SlidingWindowLimiter {
	if maxBuckets <= 0 {
		maxBuckets = 10_000
	}
	return &SlidingWindowLimiter{
		buckets:    make(map[string]*slidingBucket),
		maxBuckets: maxBuckets,
		now:        time.Now,
	}
}

// Allow evaluates and consumes 1 token for key under policy.
func (l *SlidingWindowLimiter) Allow(key string, policy RateLimitPolicy) (allowed bool, remaining int, resetDeltaSec int) {
	nowMs := l.now().UnixMilli()
	windowMs := policy.Window.Milliseconds()
	if windowMs <= 0 {
		windowMs = int64(time.Minute / time.Millisecond)
	}
	limit := policy.Limit
	if limit <= 0 {
		limit = 1
	}

	windowStart := (nowMs / windowMs) * windowMs
	resetAtMs := windowStart + windowMs
	deltaSec := int(math.Ceil(float64(resetAtMs-nowMs) / 1000.0))
	if deltaSec < 1 {
		deltaSec = 1
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= l.maxBuckets {
			l.evictOldestLocked(windowStart - windowMs)
		}
		b = &slidingBucket{windowStart: windowStart}
		l.buckets[key] = b
	} else if b.windowStart == windowStart-windowMs {
		b.windowStart = windowStart
		b.prevCount = b.currCount
		b.currCount = 0
	} else if b.windowStart != windowStart {
		b.windowStart = windowStart
		b.prevCount = 0
		b.currCount = 0
	}
	b.lastSeen = nowMs

	elapsed := float64(nowMs - windowStart)
	weight := math.Max(0, 1.0-(elapsed/float64(windowMs)))
	est := float64(b.prevCount)*weight + float64(b.currCount)

	if est+1.0 > float64(limit) {
		return false, 0, deltaSec
	}

	b.currCount++
	rem := int(math.Floor(float64(limit) - (float64(b.prevCount)*weight + float64(b.currCount))))
	if rem < 0 {
		rem = 0
	}
	return true, rem, deltaSec
}

func (l *SlidingWindowLimiter) evictOldestLocked(staleBefore int64) {
	// First sweep stale entries; if still at capacity, evict the oldest entry.
	for k, v := range l.buckets {
		if v.windowStart < staleBefore {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < l.maxBuckets {
		return
	}
	var oldestKey string
	var oldestSeen int64 = math.MaxInt64
	for k, v := range l.buckets {
		if v.lastSeen < oldestSeen {
			oldestSeen = v.lastSeen
			oldestKey = k
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// Middleware returns a Chi-compatible HTTP middleware that enforces the policy
// selected by policyFn and keyed by keyFn. Returning a nil policy skips limiting.
func (l *SlidingWindowLimiter) Middleware(
	policyFn func(r *http.Request) *RateLimitPolicy,
	keyFn func(r *http.Request) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			policy := policyFn(r)
			if policy == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("%s:%s", policy.Name, keyFn(r))
			allowed, remaining, deltaSec := l.Allow(key, *policy)
			windowSec := int(policy.Window.Seconds())
			if windowSec < 1 {
				windowSec = 1
			}

			w.Header().Set("RateLimit-Limit", strconv.Itoa(policy.Limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("RateLimit-Reset", strconv.Itoa(deltaSec))
			w.Header().Set("RateLimit-Policy", fmt.Sprintf("%d;w=%d", policy.Limit, windowSec))
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(policy.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(deltaSec))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":               "Too many requests",
					"policy":              policy.Name,
					"retry_after_seconds": deltaSec,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
