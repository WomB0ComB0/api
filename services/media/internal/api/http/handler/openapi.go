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
					"summary":  "Upload media asset",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"201": map[string]any{"description": "Asset uploaded and persisted"},
						"400": map[string]any{"description": "Invalid file or extension"},
						"401": map[string]any{"description": "Unauthorized"},
					},
				},
			},
			"/presign": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Generate direct-to-R2 presigned PUT URL",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Presigned upload URL generated"},
					},
				},
			},
			"/assets/confirm": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Confirm presigned upload",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"201": map[string]any{"description": "Asset confirmed"},
						"404": map[string]any{"description": "Object not found in R2"},
					},
				},
			},
			"/sign": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Sign private CDN URL",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Signed CDN URL generated"},
					},
				},
			},
			"/purge": map[string]any{
				"post": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Purge Cloudflare CDN cache",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Cache purged"},
					},
				},
			},
			"/assets": map[string]any{
				"get": map[string]any{
					"tags":     []string{"media"},
					"summary":  "List user assets",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Paginated asset list"},
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
						"404": map[string]any{"description": "Asset not found"},
					},
				},
				"delete": map[string]any{
					"tags":     []string{"media"},
					"summary":  "Delete asset by UUID",
					"security": []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]any{
						"204": map[string]any{"description": "Asset deleted"},
						"404": map[string]any{"description": "Asset not found"},
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
