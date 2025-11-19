package config

import "os"

type Config struct {
	Port                  string
	JWTSecret             string
	DatabaseURL           string
	R2AccountID           string
	R2AccessKeyID         string
	R2SecretAccessKey     string
	R2Bucket              string
	R2PublicURL           string
	OtelEndpoint          string
}

func Load() *Config {
	return &Config{
		Port:                  getEnv("PORT", "8080"),
		JWTSecret:             getEnv("JWT_SECRET", "development-secret-change-me"),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		R2AccountID:           getEnv("CLOUDFLARE_R2_ACCOUNT_ID", ""),
		R2AccessKeyID:         getEnv("CLOUDFLARE_R2_ACCESS_KEY_ID", ""),
		R2SecretAccessKey:     getEnv("CLOUDFLARE_R2_SECRET_ACCESS_KEY", ""),
		R2Bucket:              getEnv("CLOUDFLARE_R2_BUCKET", ""),
		R2PublicURL:           getEnv("CLOUDFLARE_R2_PUBLIC_URL", "https://cdn.mikeodnis.dev"),
		OtelEndpoint:          getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
