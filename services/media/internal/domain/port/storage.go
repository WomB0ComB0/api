package port

import (
	"context"
	"io"
	"time"
)

type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	LastModified time.Time `json:"last_modified"`
}

type ObjectMeta struct {
	Size        int64
	ContentType string
	ETag        string
}

// ObjectStore is the outbound object-storage port (implemented by Cloudflare R2 adapter).
type ObjectStore interface {
	PutObject(ctx context.Context, key string, body io.Reader, contentType string) (etag string, err error)
	HeadObject(ctx context.Context, key string) (*ObjectMeta, error)
	ReadObjectHead(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
	ListObjects(ctx context.Context, prefix string, maxKeys int32) ([]ObjectInfo, error)
	PresignPutObject(ctx context.Context, key string, contentType string, ttl time.Duration) (string, error)
	HealthCheck(ctx context.Context) error
}
