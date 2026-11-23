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

// NewRouter constructs the primary HTTP adapter (Chi router) wired to the application Factory.
func NewRouter(factory *application.Factory, cfg *config.Config) http.Handler {
	mediaHandler := handler.NewMediaHandler(factory, cfg.MaxUploadBytes)

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
	r.Use(httprate.LimitBy(100, 1*time.Minute, func(r *http.Request) (string, error) {
		if ip := chimiddleware.GetClientIP(r.Context()); ip != "" {
			return httprate.CanonicalizeIP(ip), nil
		}
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return httprate.CanonicalizeIP(host), nil
		}
		return httprate.CanonicalizeIP(r.RemoteAddr), nil
	}))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Route("/v1/media", func(r chi.Router) {
		r.Get("/health", handler.HealthCheck)
		r.Get("/health/ready", handler.ReadyCheck(factory.ObjectStore(), factory.AssetRepository()))
		r.Get("/openapi.json", handler.OpenAPISpec)

		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(cfg.JWTSecret))

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
