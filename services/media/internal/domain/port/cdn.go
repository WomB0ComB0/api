package port

import (
	"context"
	"time"
)

// URLSigner signs time-limited private CDN URLs accepted by cdn.mikeodnis.dev.
type URLSigner interface {
	SignPath(path string, expiresAt time.Time) string
	BuildPrivateURL(cdnBaseURL, r2Key string, ttl time.Duration) (url string, expiresAt time.Time)
	Verify(path string, expiresUnix string, signature string, now time.Time) bool
}

// CachePurger purges edge-cached CDN URLs.
type CachePurger interface {
	Configured() bool
	PurgeURLs(ctx context.Context, urls []string) error
}
