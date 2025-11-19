package handlers

import (
	"api/media/internal/config"
	"api/media/internal/middleware"
	"api/media/internal/storage"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

const MaxUploadSize = 100 * 1024 * 1024 // 100MB

type UploadResponse struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	URL       string    `json:"url"`
	Size      int64     `json:"size"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at"`
}

type PresignResponse struct {
	URL       string    `json:"url"`
	Key       string    `json:"key"`
	ExpiresAt time.Time `json:"expires_at"`
}

func Upload(s3Client *storage.S3Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse multipart form
		r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)
		if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
			http.Error(w, "File too large", http.StatusBadRequest)
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Failed to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Generate key
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			http.Error(w, "Failed to hash file", http.StatusInternalServerError)
			return
		}
		hashStr := hex.EncodeToString(hash.Sum(nil))
		ext := filepath.Ext(handler.Filename)
		key := fmt.Sprintf("uploads/%s/%s%s", userID, hashStr, ext)

		// Reset file pointer
		file.Seek(0, 0)

		// Upload to R2
		_, err = s3Client.Client().PutObject(context.Background(), &s3.PutObjectInput{
			Bucket:      aws.String(s3Client.Bucket()),
			Key:         aws.String(key),
			Body:        file,
			ContentType: aws.String(handler.Header.Get("Content-Type")),
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to upload to R2")
			http.Error(w, "Failed to upload file", http.StatusInternalServerError)
			return
		}

		// Return response
		response := UploadResponse{
			ID:        hashStr,
			Filename:  handler.Filename,
			URL:       fmt.Sprintf("%s/%s", cfg.R2PublicURL, key),
			Size:      handler.Size,
			MimeType:  handler.Header.Get("Content-Type"),
			CreatedAt: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
	}
}

func PresignURL(s3Client *storage.S3Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			Filename string `json:"filename"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Generate key
		ext := filepath.Ext(req.Filename)
		key := fmt.Sprintf("uploads/%s/%d%s", userID, time.Now().Unix(), ext)

		// Create presign client
		presignClient := s3.NewPresignClient(s3Client.Client())

		// Generate presigned URL
		presignedReq, err := presignClient.PresignPutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String(s3Client.Bucket()),
			Key:    aws.String(key),
		}, s3.WithPresignExpires(15*time.Minute))

		if err != nil {
			log.Error().Err(err).Msg("Failed to generate presigned URL")
			http.Error(w, "Failed to generate URL", http.StatusInternalServerError)
			return
		}

		response := PresignResponse{
			URL:       presignedReq.URL,
			Key:       key,
			ExpiresAt: time.Now().Add(15 * time.Minute),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func ListAssets(s3Client *storage.S3Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// List objects
		result, err := s3Client.Client().ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
			Bucket: aws.String(s3Client.Bucket()),
			Prefix: aws.String(fmt.Sprintf("uploads/%s/", userID)),
		})

		if err != nil {
			log.Error().Err(err).Msg("Failed to list objects")
			http.Error(w, "Failed to list assets", http.StatusInternalServerError)
			return
		}

		assets := make([]map[string]interface{}, 0)
		for _, obj := range result.Contents {
			assets = append(assets, map[string]interface{}{
				"key":          *obj.Key,
				"size":         *obj.Size,
				"last_modified": *obj.LastModified,
				"url":          fmt.Sprintf("%s/%s", cfg.R2PublicURL, *obj.Key),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"assets": assets,
		})
	}
}

func GetAsset(s3Client *storage.S3Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		// Implementation depends on your database structure
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Get asset by ID: " + id,
		})
	}
}

func DeleteAsset(s3Client *storage.S3Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		id := chi.URLParam(r, "id")
		key := fmt.Sprintf("uploads/%s/%s", userID, id)

		// Delete from R2
		_, err := s3Client.Client().DeleteObject(context.Background(), &s3.DeleteObjectInput{
			Bucket: aws.String(s3Client.Bucket()),
			Key:    aws.String(key),
		})

		if err != nil {
			log.Error().Err(err).Msg("Failed to delete object")
			http.Error(w, "Failed to delete asset", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
