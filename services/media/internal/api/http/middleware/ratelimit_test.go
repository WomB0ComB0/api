package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlidingWindowLimiterPreventsBoundaryBursts(t *testing.T) {
	limiter := NewSlidingWindowLimiter(100)
	current := time.UnixMilli(19_000) // 90% through [10s, 20s)
	limiter.now = func() time.Time { return current }
	policy := RateLimitPolicy{Name: "upload", Limit: 3, Window: 10 * time.Second}

	for i := 0; i < 3; i++ {
		if ok, _, _ := limiter.Allow("upload:user:1", policy); !ok {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}
	if ok, _, _ := limiter.Allow("upload:user:1", policy); ok {
		t.Fatalf("expected 4th request in same window to be blocked")
	}

	// 500ms past window boundary -> 95% weight on prev 3 = 2.85; 2.85 + 1 > 3 -> blocked!
	current = time.UnixMilli(20_500)
	if ok, _, _ := limiter.Allow("upload:user:1", policy); ok {
		t.Fatalf("expected boundary burst request to be blocked by sliding-window counter")
	}

	// Halfway through next window -> 50% weight on prev 3 = 1.5 -> 1 allowed, 2nd blocked
	current = time.UnixMilli(25_000)
	if ok, _, _ := limiter.Allow("upload:user:1", policy); !ok {
		t.Fatalf("expected request at 50%% of next window to be allowed")
	}
	if ok, _, _ := limiter.Allow("upload:user:1", policy); ok {
		t.Fatalf("expected 2nd request at 50%% of next window to be blocked")
	}
}

func TestRateLimitMiddlewareEmitsStandardHeadersAndPerUserIsolation(t *testing.T) {
	limiter := NewSlidingWindowLimiter(100)
	mw := limiter.Middleware(
		func(r *http.Request) *RateLimitPolicy {
			if r.URL.Path == "/v1/media/health" {
				return nil
			}
			return &RateLimitPolicy{Name: "media-write", Limit: 2, Window: time.Minute}
		},
		func(r *http.Request) string {
			if uid := GetUserID(r.Context()); uid != "" {
				return "user:" + uid
			}
			return "ip:unknown"
		},
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(path, userID string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if userID != "" {
			ctx := context.WithValue(req.Context(), userIDKey, userID)
			req = req.WithContext(ctx)
		}
		handler.ServeHTTP(rec, req)
		return rec
	}

	r1 := makeReq("/v1/media/upload", "user-1")
	if r1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", r1.Code)
	}
	if got := r1.Header().Get("RateLimit-Policy"); got != "2;w=60" {
		t.Fatalf("expected RateLimit-Policy=2;w=60, got %q", got)
	}

	r2 := makeReq("/v1/media/upload", "user-1")
	if r2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", r2.Code)
	}

	// 3rd request for user-1 blocked with 429 + Retry-After
	r3 := makeReq("/v1/media/upload", "user-1")
	if r3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", r3.Code)
	}
	if r3.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on 429")
	}

	// Different authenticated user is isolated from user-1's quota
	otherUser := makeReq("/v1/media/upload", "user-2")
	if otherUser.Code != http.StatusOK {
		t.Fatalf("expected 200 for separate user, got %d", otherUser.Code)
	}

	// Health endpoint bypasses rate limiting
	health := makeReq("/v1/media/health", "user-1")
	if health.Code != http.StatusOK {
		t.Fatalf("expected 200 for exempt health endpoint, got %d", health.Code)
	}
}
