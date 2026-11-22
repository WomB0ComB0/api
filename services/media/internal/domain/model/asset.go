package model

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"api/media/internal/domain/event"
	"api/media/internal/pkg/apperr"
)

// Visibility value object enforces public/private CDN access modes.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

func ParseVisibility(raw string) (Visibility, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(VisibilityPublic):
		return VisibilityPublic, nil
	case string(VisibilityPrivate):
		return VisibilityPrivate, nil
	default:
		return "", apperr.Validation("visibility must be 'public' or 'private'")
	}
}

var allowedExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true,
	".svg": true, ".ico": true,
	".pdf": true, ".txt": true, ".csv": true, ".json": true, ".md": true,
	".mp4": true, ".webm": true, ".mp3": true, ".wav": true, ".ogg": true,
	".zip": true, ".woff2": true,
}

// Filename value object guarantees sanitized leaf filename + allowed extension.
type Filename struct {
	Base string
	Ext  string
}

func ParseFilename(raw string) (Filename, error) {
	base := filepath.Base(strings.TrimSpace(raw))
	if base == "" || base == "." || base == "/" || strings.Contains(base, "..") || strings.ContainsAny(base, `/\`) {
		return Filename{}, apperr.Validation("invalid filename")
	}
	ext := strings.ToLower(filepath.Ext(base))
	if !allowedExtensions[ext] {
		return Filename{}, apperr.Validation(fmt.Sprintf("file extension %q is not allowed", ext))
	}
	return Filename{Base: base, Ext: ext}, nil
}

// BuildObjectKey produces an R2 key matching cdn.mikeodnis.dev Worker routes:
//   - public  -> assets/<user_id>/<leaf>
//   - private -> private/<user_id>/<leaf>
func BuildObjectKey(vis Visibility, userID, leaf string) string {
	prefix := "assets"
	if vis == VisibilityPrivate {
		prefix = "private"
	}
	return fmt.Sprintf("%s/%s/%s", prefix, userID, leaf)
}

// ValidateOwnedKey enforces that an object key contains no traversal and belongs to userID.
func ValidateOwnedKey(key, userID string) (Visibility, error) {
	clean := strings.TrimLeft(strings.TrimSpace(key), "/")
	if clean == "" || strings.Contains(clean, "..") || strings.Contains(clean, `\`) {
		return "", apperr.Validation("invalid object key")
	}
	if strings.HasPrefix(clean, fmt.Sprintf("assets/%s/", userID)) {
		return VisibilityPublic, nil
	}
	if strings.HasPrefix(clean, fmt.Sprintf("private/%s/", userID)) {
		return VisibilityPrivate, nil
	}
	return "", apperr.Forbidden("key does not belong to authenticated user")
}

// Asset is the core media domain aggregate root.
type Asset struct {
	ID         string
	UserID     string
	Filename   string
	MimeType   string
	SizeBytes  int64
	R2Key      string
	PublicURL  string
	Visibility Visibility
	ETag       string
	SHA256     string
	Metadata   map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time

	events []event.Event
}

func NewAsset(
	userID string,
	filename Filename,
	mimeType string,
	sizeBytes int64,
	r2Key string,
	publicURL string,
	visibility Visibility,
	etag string,
	sha256Hex string,
	metadata map[string]any,
) (*Asset, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, apperr.Validation("user_id is required")
	}
	if sizeBytes < 0 {
		return nil, apperr.Validation("size_bytes must be non-negative")
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	now := time.Now().UTC()
	return &Asset{
		UserID:     userID,
		Filename:   filename.Base,
		MimeType:   mimeType,
		SizeBytes:  sizeBytes,
		R2Key:      r2Key,
		PublicURL:  publicURL,
		Visibility: visibility,
		ETag:       etag,
		SHA256:     sha256Hex,
		Metadata:   metadata,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (a *Asset) CanAccess(actorUserID, actorRole string) bool {
	return actorRole == "admin" || a.UserID == actorUserID
}

func (a *Asset) MarkUploaded() {
	a.events = append(a.events, event.NewAssetUploadedEvent(
		a.ID, a.UserID, a.R2Key, a.PublicURL, string(a.Visibility), a.SizeBytes,
	))
}

func (a *Asset) MarkDeleted() {
	a.events = append(a.events, event.NewAssetDeletedEvent(
		a.ID, a.UserID, a.R2Key, a.PublicURL, string(a.Visibility),
	))
}

// PullEvents drains and returns pending domain events from this aggregate.
func (a *Asset) PullEvents() []event.Event {
	out := a.events
	a.events = nil
	return out
}
