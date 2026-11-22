package cloudflare

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestURLSignerRoundTripAndExpiry(t *testing.T) {
	signer := NewURLSigner("super-secret-signing-key-32-bytes!!")
	now := time.Now().UTC()

	signedURL, exp := signer.BuildPrivateURL("https://cdn.mikeodnis.dev", "private/user-1/file.pdf", 5*time.Minute)
	if !strings.HasPrefix(signedURL, "https://cdn.mikeodnis.dev/private/user-1/file.pdf?") {
		t.Fatalf("unexpected signed URL prefix: %s", signedURL)
	}

	parsed, err := url.Parse(signedURL)
	if err != nil {
		t.Fatalf("failed to parse signed URL: %v", err)
	}

	sig := parsed.Query().Get("sig")
	expStr := parsed.Query().Get("exp")
	if strings.Contains(sig, "=") {
		t.Fatalf("expected unpadded base64url signature for Cloudflare Worker compatibility, got %s", sig)
	}

	if !signer.Verify(parsed.Path, expStr, sig, now) {
		t.Fatalf("expected valid signature to verify")
	}
	if signer.Verify(parsed.Path, expStr, sig+"x", now) {
		t.Fatalf("expected tampered signature to fail")
	}
	if signer.Verify("/private/user-1/other.pdf", expStr, sig, now) {
		t.Fatalf("expected signature for different path to fail")
	}
	if signer.Verify(parsed.Path, expStr, sig, exp.Add(time.Minute)) {
		t.Fatalf("expected expired signature to fail")
	}
}
