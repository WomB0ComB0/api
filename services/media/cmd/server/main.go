package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"api/media/internal/adapter/cloudflare"
	"api/media/internal/adapter/postgres"
	"api/media/internal/adapter/r2"
	"api/media/internal/adapter/telemetry"
	httpapi "api/media/internal/api/http"
	"api/media/internal/application"
	"api/media/internal/config"
	"api/media/internal/domain/event"
	"api/media/internal/domain/port"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("GO_ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// 1. Load & validate configuration
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("Invalid configuration")
	}

	// 2. Initialize secondary adapters (Telemetry, R2, PostgreSQL, Cloudflare CDN)
	shutdownTelemetry, err := telemetry.InitTelemetry(cfg.OtelEndpoint, "media-service")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize telemetry")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(ctx)
	}()

	objectStore, err := r2.NewObjectStore(context.Background(), cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize R2 object store")
	}

	var assetRepo port.AssetRepository
	if cfg.DatabaseURL != "" {
		pgRepo, err := postgres.NewAssetRepository(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize PostgreSQL asset repository")
		}
		assetRepo = pgRepo
		defer pgRepo.Close()
	} else {
		log.Warn().Msg("DATABASE_URL not set; asset metadata persistence disabled")
	}

	signer := cloudflare.NewURLSigner(cfg.SigningSecret)
	purger := cloudflare.NewCachePurger(cfg.CloudflareZoneID, cfg.CloudflareAPIToken)

	// 3. Wire Domain Event Bus + Subscribers
	eventBus := event.NewInMemoryEventBus()
	eventBus.Subscribe(cloudflare.NewAssetDeletedPurgeSubscriber(purger))

	// 4. Wire Application Use Case Factory
	appFactory := application.NewFactory(
		objectStore,
		assetRepo,
		signer,
		purger,
		eventBus,
		cfg.R2PublicURL,
		cfg.MaxUploadBytes,
	)

	// 5. Wire Primary HTTP Adapter
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           httpapi.NewRouter(appFactory, cfg),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	go func() {
		log.Info().Msgf("Media service (Hexagonal) listening on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down media service...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}
	log.Info().Msg("Media service exited cleanly")
}
