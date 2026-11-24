package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Env                string
	Port               string
	JWTSecret          string
	SigningSecret      string
	DatabaseURL        string
	R2AccountID        string
	R2AccessKeyID      string
	R2SecretAccessKey  string
	R2Bucket           string
	R2Endpoint         string
	R2PublicURL        string
	CloudflareZoneID   string
	CloudflareAPIToken string
	CORSOrigins        []string
	MaxUploadBytes     int64
	OtelEndpoint       string
}

const (
	defaultDevJWTSecret     = "development-jwt-secret-change-me-32chars!"
	defaultDevSigningSecret = "development-signing-secret-change-me-32b!"
	defaultMaxUploadBytes   = 100 * 1024 * 1024 // 100MB
)

func Load() *Config {
	env := getEnv("GO_ENV", "development")

	jwtDefault := ""
	signingDefault := ""
	if env != "production" {
		jwtDefault = defaultDevJWTSecret
		signingDefault = defaultDevSigningSecret
	}

	accountID := getEnv("CLOUDFLARE_R2_ACCOUNT_ID", getEnv("R2_ACCOUNT_ID", ""))
	r2Endpoint := getEnv("CLOUDFLARE_R2_ENDPOINT", getEnv("R2_ENDPOINT", ""))
	if r2Endpoint == "" && accountID != "" {
		r2Endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	}

	maxUpload := int64(defaultMaxUploadBytes)
	if raw := os.Getenv("MAX_UPLOAD_BYTES"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			maxUpload = parsed
		}
	}

	origins := parseOrigins(os.Getenv("CORS_ORIGINS"), env)

	return &Config{
		Env:                env,
		Port:               getEnv("PORT", "8080"),
		JWTSecret:          getEnv("JWT_SECRET", jwtDefault),
		SigningSecret:      getEnv("SIGNING_SECRET", signingDefault),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		R2AccountID:        accountID,
		R2AccessKeyID:      getEnv("CLOUDFLARE_R2_ACCESS_KEY_ID", getEnv("R2_ACCESS_KEY_ID", "")),
		R2SecretAccessKey:  getEnv("CLOUDFLARE_R2_SECRET_ACCESS_KEY", getEnv("R2_SECRET_ACCESS_KEY", "")),
		R2Bucket:           getEnv("CLOUDFLARE_R2_BUCKET", getEnv("R2_BUCKET_NAME", "")),
		R2Endpoint:         r2Endpoint,
		R2PublicURL:        strings.TrimRight(getEnv("CLOUDFLARE_R2_PUBLIC_URL", "https://cdn.mikeodnis.dev"), "/"),
		CloudflareZoneID:   getEnv("CLOUDFLARE_ZONE_ID", ""),
		CloudflareAPIToken: getEnv("CLOUDFLARE_API_TOKEN", ""),
		CORSOrigins:        origins,
		MaxUploadBytes:     maxUpload,
		OtelEndpoint:       getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}
}

func (c *Config) Validate() error {
	if c.MaxUploadBytes <= 0 {
		return fmt.Errorf("MAX_UPLOAD_BYTES must be positive")
	}
	if c.Env == "production" {
		if len(c.JWTSecret) < 32 || c.JWTSecret == defaultDevJWTSecret {
			return fmt.Errorf("JWT_SECRET must be a non-default secret of at least 32 bytes in production")
		}
		if len(c.SigningSecret) < 32 || c.SigningSecret == defaultDevSigningSecret {
			return fmt.Errorf("SIGNING_SECRET must be a non-default secret of at least 32 bytes in production")
		}
		if c.JWTSecret == c.SigningSecret {
			return fmt.Errorf("JWT_SECRET and SIGNING_SECRET must be distinct in production")
		}
		if c.R2Bucket == "" || c.R2Endpoint == "" || c.R2AccessKeyID == "" || c.R2SecretAccessKey == "" {
			return fmt.Errorf("CLOUDFLARE_R2_BUCKET, ENDPOINT, ACCESS_KEY_ID, and SECRET_ACCESS_KEY are required in production")
		}
	}
	return nil
}

func parseOrigins(raw, env string) []string {
	if strings.TrimSpace(raw) != "" {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	origins := []string{
		"https://mikeodnis.dev",
		"https://www.mikeodnis.dev",
		"https://cdn.mikeodnis.dev",
		"https://blog.mikeodnis.dev",
		"https://news.mikeodnis.dev",
		"https://links.mikeodnis.dev",
	}
	if env != "production" {
		origins = append(origins, "http://localhost:3000", "http://localhost:8080", "http://localhost")
	}
	return origins
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
