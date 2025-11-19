package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"api/media/internal/config"
	"api/media/internal/handlers"
	"api/media/internal/middleware"
	"api/media/internal/storage"
	"api/media/internal/telemetry"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Initialize logger
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("GO_ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// Load config
	cfg := config.Load()

	// Initialize telemetry
	shutdown, err := telemetry.InitTelemetry(cfg.OtelEndpoint, "media-service")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize telemetry")
	}
	defer shutdown(context.Background())

	// Initialize storage client
	s3Client, err := storage.NewS3Client(context.Background(), cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize S3 client")
	}

	// Initialize router
	r := chi.NewRouter()

	// Middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))
	r.Use(httprate.LimitByIP(100, 1*time.Minute))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://mikeodnis.dev", "https://www.mikeodnis.dev"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Routes
	r.Route("/v1/media", func(r chi.Router) {
		// Public routes
		r.Get("/health", handlers.HealthCheck)
		r.Get("/openapi.json", handlers.OpenAPISpec)

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(cfg.JWTSecret))

			// Upload
			r.Post("/upload", handlers.Upload(s3Client, cfg))

			// Pre-signed URLs
			r.Post("/presign", handlers.PresignURL(s3Client, cfg))

			// List assets
			r.Get("/assets", handlers.ListAssets(s3Client, cfg))

			// Get asset metadata
			r.Get("/assets/{id}", handlers.GetAsset(s3Client, cfg))

			// Delete asset
			r.Delete("/assets/{id}", handlers.DeleteAsset(s3Client, cfg))
		})
	})

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Info().Msgf("🚀 Media service listening on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Server exited")
}
