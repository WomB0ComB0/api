package handler

import "net/http"

func OpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Media API",
			"description": "Hexagonal Go media service: R2 uploads, direct presigning, cdn.mikeodnis.dev HMAC signed URLs, and edge cache purging",
			"version":     "1.0.0",
		},
		"servers": []map[string]string{
			{"url": "https://api.mikeodnis.dev/v1/media", "description": "Production"},
			{"url": "http://localhost/v1/media", "description": "Local Development"},
		},
		"tags": []map[string]string{
			{"name": "health", "description": "Health and readiness endpoints"},
			{"name": "media", "description": "Media asset management and CDN integration"},
		},
		"paths": map[string]any{
			"/health": map[string]any{
				"get": map[string]any{
					"tags":    []string{"health"},
					"summary": "Liveness check",
					"responses": map[string]any{
						"200": map[string]any{"description": "Service is healthy"},
					},
				},
			},
			"/health/ready": map[string]any{
				"get": map[string]any{
					"tags":    []string{"health"},
					"summary": "Readiness check (R2 + PostgreSQL)",
					"responses": map[string]any{
						"200": map[string]any{"description": "Service is ready"},
						"503": map[string]any{"description": "One or more dependencies unhealthy"},
					},
				},
			},
			"/upload": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Upload media asset (WSTG-BUSL-08/09 magic-byte + SVG verified)",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"201": map[string]any{"description": "Asset uploaded and persisted"},
						"400": map[string]any{"description": "Invalid file, filename, or magic-byte/MIME mismatch"},
						"401": map[string]any{"description": "Unauthorized"},
						"413": map[string]any{"description": "File exceeds maximum allowed size"},
					},
				},
			},
			"/presign": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Generate direct-to-R2 presigned PUT URL targeting non-servable staging/* prefix",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Presigned staging upload URL generated"},
						"400": map[string]any{"description": "Disallowed extension or MIME mismatch"},
					},
				},
			},
			"/assets/confirm": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Verify staged upload (ETag-pinned magic-byte + SVG scan) and atomically promote to live prefix",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"201": map[string]any{"description": "Asset verified, promoted, and persisted"},
						"400": map[string]any{"description": "Invalid signature, SVG active content, or metadata"},
						"404": map[string]any{"description": "Staged object not found in R2"},
						"409": map[string]any{"description": "Upload already confirmed or object mutated during verification (ETag mismatch)"},
						"413": map[string]any{"description": "Staged object exceeds maximum allowed size (purged)"},
					},
				},
			},
			"/sign": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Sign private/* CDN URL (HMAC-SHA256)",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Signed private CDN URL generated"},
						"400": map[string]any{"description": "Non-private asset/key, invalid TTL, or unsafe object key"},
						"401": map[string]any{"description": "Unauthorized"},
						"403": map[string]any{"description": "Forbidden"},
						"429": map[string]any{"description": "Rate limit exceeded"},
					},
				},
			},
			"/purge": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Purge Cloudflare CDN cache (owner-scoped or admin)",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Cache purged"},
						"400": map[string]any{"description": "Invalid CDN URL or path traversal"},
						"403": map[string]any{"description": "Forbidden to purge another user's assets"},
						"429": map[string]any{"description": "Rate limit exceeded (20/min write tier)"},
						"503": map[string]any{"description": "Cloudflare zone purge not configured"},
					},
				},
			},
			"/assets": map[string]any{
				"get": map[string]any{
					"tags":     []string{"media"},
					"summary":  "List user assets (limit <= 100, offset <= 10000)",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Paginated asset list"},
						"400": map[string]any{"description": "Offset exceeds 10000"},
						"401": map[string]any{"description": "Unauthorized"},
					},
				},
			},
			"/assets/{id}": map[string]any{
				"get": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Get asset metadata by UUID",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Asset metadata"},
						"403": map[string]any{"description": "Forbidden"},
						"404": map[string]any{"description": "Asset not found"},
					},
				},
				"delete": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Delete asset by UUID (R2 + DB + event-driven cache purge; 20/min write tier)",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"204": map[string]any{"description": "Asset deleted"},
						"403": map[string]any{"description": "Forbidden"},
						"404": map[string]any{"description": "Asset not found"},
						"429": map[string]any{"description": "Rate limit exceeded"},
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]string{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
	}

	writeJSON(w, http.StatusOK, spec)
}
