package httpapi

import (
	"net"
	"net/http"
	"time"

	"api/media/internal/api/http/handler"
	"api/media/internal/api/http/middleware"
	"api/media/internal/application"
	"api/media/internal/config"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func resolveCanonicalIP(r *http.Request) string {
	if ip := chimiddleware.GetClientIP(r.Context()); ip != "" {
		return httprate.CanonicalizeIP(ip)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return httprate.CanonicalizeIP(host)
	}
	return httprate.CanonicalizeIP(r.RemoteAddr)
}

// NewRouter constructs the primary HTTP adapter (Chi router) wired to the application Factory.
func NewRouter(factory *application.Factory, cfg *config.Config) http.Handler {
	mediaHandler := handler.NewMediaHandler(factory, cfg.MaxUploadBytes)
	ipLimiter := middleware.NewSlidingWindowLimiter(10_000)
	userLimiter := middleware.NewSlidingWindowLimiter(10_000)

	ipCeilingPolicy := middleware.RateLimitPolicy{
		Name:   "media-ip",
		Limit:  120,
		Window: 1 * time.Minute,
	}
	userWritePolicy := middleware.RateLimitPolicy{
		Name:   "media-write",
		Limit:  20,
		Window: 1 * time.Minute,
	}
	userReadPolicy := middleware.RateLimitPolicy{
		Name:   "media-read",
		Limit:  100,
		Window: 1 * time.Minute,
	}

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	// Fail-closed XFF walk trusting only private mesh & loopback reverse proxies (Railway, Docker, localhost)
	r.Use(chimiddleware.ClientIPFromXFF(
		"127.0.0.1/32",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"::1/128",
		"fc00::/7",
	))
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders: []string{
			"X-Request-ID",
			"RateLimit-Limit",
			"RateLimit-Remaining",
			"RateLimit-Reset",
			"RateLimit-Policy",
			"Retry-After",
			"X-RateLimit-Limit",
			"X-RateLimit-Remaining",
		},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Outer IP ceiling (exempts health & OpenAPI probes)
	r.Use(ipLimiter.Middleware(
		func(r *http.Request) *middleware.RateLimitPolicy {
			switch r.URL.Path {
			case "/v1/media/health", "/v1/media/health/ready", "/v1/media/openapi.json":
				return nil
			default:
				return &ipCeilingPolicy
			}
		},
		func(r *http.Request) string {
			return "ip:" + resolveCanonicalIP(r)
		},
	))

	r.Route("/v1/media", func(r chi.Router) {
		r.Get("/health", handler.HealthCheck)
		r.Get("/health/ready", handler.ReadyCheck(factory.ObjectStore(), factory.AssetRepository()))
		r.Get("/openapi.json", handler.OpenAPISpec)

		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(cfg.JWTSecret))
			// Post-auth per-user cost-tiered sliding-window limits (upload/presign/purge tighter than reads)
			r.Use(userLimiter.Middleware(
				func(r *http.Request) *middleware.RateLimitPolicy {
					if r.Method == http.MethodPost {
						switch r.URL.Path {
						case "/v1/media/upload", "/v1/media/presign", "/v1/media/assets/confirm", "/v1/media/purge":
							return &userWritePolicy
						}
					}
					return &userReadPolicy
				},
				func(r *http.Request) string {
					if uid := middleware.GetUserID(r.Context()); uid != "" {
						return "user:" + uid
					}
					return "ip:" + resolveCanonicalIP(r)
				},
			))

			r.Post("/upload", mediaHandler.Upload)
			r.Post("/presign", mediaHandler.PresignUpload)
			r.Post("/assets/confirm", mediaHandler.ConfirmUpload)
			r.Post("/sign", mediaHandler.SignURL)
			r.Post("/purge", mediaHandler.PurgeCache)
			r.Get("/assets", mediaHandler.ListAssets)
			r.Get("/assets/{id}", mediaHandler.GetAsset)
			r.Delete("/assets/{id}", mediaHandler.DeleteAsset)
		})
	})

	if cfg.OtelEndpoint != "" {
		return otelhttp.NewHandler(r, "media-service")
	}
	return r
}
