package model

import (
	"testing"

	"api/media/internal/domain/event"
	"api/media/internal/pkg/apperr"
)

func TestValueObjectsAndInvariants(t *testing.T) {
	if _, err := ParseFilename("malware.exe"); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected validation error for .exe, got %v", err)
	}
	if _, err := ParseFilename("../secret.png"); err != nil {
		// filepath.Base strips directory components safely
		t.Fatalf("unexpected error for sanitized filename: %v", err)
	}
	fn, err := ParseFilename("Avatar.PNG")
	if err != nil || fn.Ext != ".png" {
		t.Fatalf("expected valid .png filename, got %+v err=%v", fn, err)
	}

	vis, err := ParseVisibility("")
	if err != nil || vis != VisibilityPublic {
		t.Fatalf("expected default public visibility, got %q err=%v", vis, err)
	}
	if _, err := ParseVisibility("unlisted"); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected validation error for unknown visibility, got %v", err)
	}

	if _, err := ValidateOwnedKey("assets/u1/../../etc/passwd", "u1"); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
	if _, err := ValidateOwnedKey("assets/u2/file.png", "u1"); !apperr.IsType(err, apperr.TypeForbidden) {
		t.Fatalf("expected forbidden for other user's key, got %v", err)
	}

	asset, err := NewAsset("u1", fn, "image/png", 128, "assets/u1/abc.png", "https://cdn.mikeodnis.dev/assets/u1/abc.png", VisibilityPublic, "etag", "sha", nil)
	if err != nil {
		t.Fatalf("unexpected error creating asset: %v", err)
	}
	asset.ID = "asset-1"
	if !asset.CanAccess("u1", "user") || !asset.CanAccess("u2", "admin") || asset.CanAccess("u2", "user") {
		t.Fatalf("unexpected access control result")
	}

	asset.MarkUploaded()
	asset.MarkDeleted()
	evts := asset.PullEvents()
	if len(evts) != 2 || evts[0].EventName() != event.EventAssetUploaded || evts[1].EventName() != event.EventAssetDeleted {
		t.Fatalf("unexpected events: %+v", evts)
	}
	if len(asset.PullEvents()) != 0 {
		t.Fatalf("expected PullEvents to drain queue")
	}
}

func TestFilenameAndUploadTypeSafety(t *testing.T) {
	// Filename hygiene: bidi overrides (CVE-2021-42574), NUL, trailing dots, Windows reserved names, dotfiles
	badNames := []string{
		"invoice\u202Egnp.png",
		"evil\x00.png",
		"report.pdf.",
		"CON.txt",
		".hidden.png",
		string(make([]byte, 260)) + ".png",
	}
	for _, name := range badNames {
		if _, err := ParseFilename(name); !apperr.IsType(err, apperr.TypeValidation) {
			t.Fatalf("expected validation error for unsafe filename %q, got %v", name, err)
		}
	}

	// Zero-byte asset must fail invariant
	validFn, _ := ParseFilename("avatar.png")
	if _, err := NewAsset("u1", validFn, "image/png", 0, "assets/u1/a.png", "https://cdn.mikeodnis.dev/assets/u1/a.png", VisibilityPublic, "etag", "sha", nil); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected zero-byte asset rejection, got %v", err)
	}

	// Spoofed HTML named fake.png -> rejected (WSTG-BUSL-08/09)
	if _, err := ValidateUploadContent(validFn, "image/png", []byte("<html><script>alert(1)</script></html>")); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected HTML spoof as .png to be rejected, got %v", err)
	}

	// PHP source disguised as .jpg -> rejected
	jpgFn, _ := ParseFilename("shell.php.jpg")
	if _, err := ValidateUploadContent(jpgFn, "image/jpeg", []byte("<?php system($_GET['cmd']); ?>")); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected PHP polyglot as .jpg to be rejected, got %v", err)
	}

	// Valid PNG bytes with spoofed declared Content-Type -> rejected
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-png-body")
	if _, err := ValidateUploadContent(validFn, "text/html", pngBytes); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected declared MIME mismatch rejection, got %v", err)
	}
	// Valid PNG bytes with canonical or generic octet-stream MIME -> accepted
	if ct, err := ValidateUploadContent(validFn, "application/octet-stream", pngBytes); err != nil || ct != "image/png" {
		t.Fatalf("expected canonical image/png, got ct=%q err=%v", ct, err)
	}

	// SVG XSS / XXE rejection vs clean SVG acceptance
	svgFn, _ := ParseFilename("icon.svg")
	for _, badSVG := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><circle r="5"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body></body></foreignObject></svg>`,
		`<!DOCTYPE svg [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><svg>&xxe;</svg>`,
	} {
		if _, err := ValidateUploadContent(svgFn, "image/svg+xml", []byte(badSVG)); !apperr.IsType(err, apperr.TypeValidation) {
			t.Fatalf("expected malicious SVG rejection for %q, got %v", badSVG, err)
		}
	}
	cleanSVG := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`
	if ct, err := ValidateUploadContent(svgFn, "image/svg+xml", []byte(cleanSVG)); err != nil || ct != "image/svg+xml" {
		t.Fatalf("expected clean SVG acceptance, got ct=%q err=%v", ct, err)
	}

	// AVIF, WOFF2, JSON canonical validation
	avifFn, _ := ParseFilename("hero.avif")
	if ct, err := ValidateUploadContent(avifFn, "image/avif", []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00")); err != nil || ct != "image/avif" {
		t.Fatalf("expected valid AVIF, got ct=%q err=%v", ct, err)
	}
	woffFn, _ := ParseFilename("geist.woff2")
	if ct, err := ValidateUploadContent(woffFn, "font/woff2", []byte("wOF2\x00\x01\x00\x00payload")); err != nil || ct != "font/woff2" {
		t.Fatalf("expected valid WOFF2, got ct=%q err=%v", ct, err)
	}
	jsonFn, _ := ParseFilename("data.json")
	if ct, err := ValidateUploadContent(jsonFn, "application/json", []byte(`{"hello":"world"}`)); err != nil || ct != "application/json" {
		t.Fatalf("expected valid JSON, got ct=%q err=%v", ct, err)
	}

	// Presign allowlist blocks mismatched extension <-> MIME
	if _, err := ValidatePresignContentType(validFn, "image/svg+xml"); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected presign MIME mismatch rejection, got %v", err)
	}
}
