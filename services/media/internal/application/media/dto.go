package media

import (
	"io"
	"time"

	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
)

const (
	DefaultPresignTTL   = 15 * time.Minute
	DefaultSignedURLTTL = 1 * time.Hour
	MinSignedURLTTLSecs = 60
	MaxSignedURLTTLSecs = 86400
)

type Actor struct {
	UserID string
	Role   string
}

type AssetDTO struct {
	ID         string         `json:"id"`
	Filename   string         `json:"filename"`
	Key        string         `json:"key"`
	URL        string         `json:"url"`
	Visibility string         `json:"visibility"`
	Size       int64          `json:"size"`
	MimeType   string         `json:"mime_type"`
	ETag       string         `json:"etag,omitempty"`
	SHA256     string         `json:"sha256,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

func ToAssetDTO(a *model.Asset, cdnBaseURL string, signer port.URLSigner) AssetDTO {
	url := a.PublicURL
	if a.Visibility == model.VisibilityPrivate && signer != nil {
		url, _ = signer.BuildPrivateURL(cdnBaseURL, a.R2Key, DefaultSignedURLTTL)
	}
	return AssetDTO{
		ID:         a.ID,
		Filename:   a.Filename,
		Key:        a.R2Key,
		URL:        url,
		Visibility: string(a.Visibility),
		Size:       a.SizeBytes,
		MimeType:   a.MimeType,
		ETag:       a.ETag,
		SHA256:     a.SHA256,
		Metadata:   a.Metadata,
		CreatedAt:  a.CreatedAt,
	}
}

type UploadInput struct {
	Actor             Actor
	Filename          string
	HeaderContentType string
	Visibility        string
	File              io.ReadSeeker
}

type PresignInput struct {
	Actor       Actor
	Filename    string
	ContentType string
	Visibility  string
}

type PresignOutput struct {
	UploadURL  string    `json:"upload_url"`
	URL        string    `json:"url"`
	Key        string    `json:"key"`
	PublicURL  string    `json:"public_url"`
	Visibility string    `json:"visibility"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type ConfirmUploadInput struct {
	Actor    Actor
	Key      string
	Filename string
	Metadata map[string]any
}

type GetAssetInput struct {
	Actor   Actor
	AssetID string
}

type ListAssetsInput struct {
	Actor  Actor
	Limit  int
	Offset int
}

type ListAssetsOutput struct {
	Assets []AssetDTO `json:"assets"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

type DeleteAssetInput struct {
	Actor   Actor
	AssetID string
}

type SignURLInput struct {
	Actor     Actor
	AssetID   string
	Key       string
	ExpiresIn int64
}

type SignURLOutput struct {
	URL       string    `json:"url"`
	Key       string    `json:"key"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PurgeCacheInput struct {
	Actor Actor
	Files []string
}

type PurgeCacheOutput struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}
