package handlers

import (
	"encoding/json"
	"net/http"
)

func OpenAPISpec(w http.ResponseWriter, r *http.Request) {
	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "Media API",
			"description": "High-performance media operations, streaming, and R2 pre-signing",
			"version":     "1.0.0",
		},
		"servers": []map[string]string{
			{"url": "https://api.mikeodnis.dev/v1/media", "description": "Production"},
		},
		"tags": []map[string]string{
			{"name": "health", "description": "Health check endpoints"},
			{"name": "media", "description": "Media operations"},
		},
		"paths": map[string]interface{}{
			"/health": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"health"},
					"summary":     "Health check",
					"description": "Check service health",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Service is healthy",
						},
					},
				},
			},
			"/upload": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"media"},
					"summary":     "Upload media",
					"description": "Upload media file to R2",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"requestBody": map[string]interface{}{
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"file": map[string]string{
											"type":   "string",
											"format": "binary",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{
							"description": "File uploaded successfully",
						},
					},
				},
			},
			"/presign": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"media"},
					"summary":     "Generate pre-signed URL",
					"description": "Generate pre-signed URL for upload",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Pre-signed URL generated",
						},
					},
				},
			},
			"/assets": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"media"},
					"summary":     "List assets",
					"description": "List all media assets",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "List of assets",
						},
					},
				},
			},
			"/assets/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"media"},
					"summary":     "Get asset",
					"description": "Get asset metadata",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Asset ID",
							"schema":      map[string]string{"type": "string", "format": "uuid"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Asset metadata",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"media"},
					"summary":     "Delete asset",
					"description": "Delete media asset",
					"security":    []map[string][]string{{"bearerAuth": {}}},
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Asset ID",
							"schema":      map[string]string{"type": "string", "format": "uuid"},
						},
					},
					"responses": map[string]interface{}{
						"204": map[string]interface{}{
							"description": "Asset deleted",
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]string{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}
