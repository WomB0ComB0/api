package cloudflare

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/media/internal/domain/port"
)

var _ port.URLSigner = (*URLSigner)(nil)

// URLSigner creates and verifies HMAC-SHA256 signatures compatible with cdn.mikeodnis.dev's Cloudflare Worker.
// Signature message format: "<path>:<unix_expires>" -> unpadded Base64URL.
type URLSigner struct {
	secret []byte
}

func NewURLSigner(secret string) *URLSigner {
	return &URLSigner{secret: []byte(secret)}
}

func (s *URLSigner) SignPath(path string, expiresAt time.Time) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	message := fmt.Sprintf("%s:%d", path, expiresAt.Unix())
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *URLSigner) BuildPrivateURL(cdnBaseURL, r2Key string, ttl time.Duration) (string, time.Time) {
	base := strings.TrimRight(cdnBaseURL, "/")
	cleanKey := strings.TrimLeft(r2Key, "/")
	path := "/" + cleanKey
	if !strings.HasPrefix(cleanKey, "private/") {
		path = "/private/" + cleanKey
	}

	expiresAt := time.Now().UTC().Add(ttl)
	sig := s.SignPath(path, expiresAt)
	return fmt.Sprintf("%s%s?exp=%d&sig=%s", base, path, expiresAt.Unix(), sig), expiresAt
}

func (s *URLSigner) Verify(path string, expiresUnix string, signature string, now time.Time) bool {
	if len(s.secret) == 0 || signature == "" || expiresUnix == "" {
		return false
	}
	exp, err := strconv.ParseInt(expiresUnix, 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	expected := s.SignPath(path, time.Unix(exp, 0))
	return hmac.Equal([]byte(expected), []byte(signature))
}
