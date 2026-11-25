package model

import (
	"archive/zip"
	"bytes"
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
	// Query/fragment injection, empty leaf, double-slash, and bidi controls in keys must be rejected
	for _, badKey := range []string{
		"private/u1/",
		"private/u1/file.png?admin=1",
		"private/u1/file.png#frag",
		"private/u1/file%2e%2e.png",
		"private/u1/a//b.png",
		"private/u1/bad\u202E.png",
	} {
		if _, err := ValidateOwnedKey(badKey, "u1"); !apperr.IsType(err, apperr.TypeValidation) {
			t.Fatalf("expected validation error for unsafe key %q, got %v", badKey, err)
		}
	}
	if _, _, err := ValidateStagingKey("staging/assets/u1/sub/nested.png", "u1"); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected nested staging key rejection, got %v", err)
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

	// SVG XSS / XXE / entity-evasion / animation / external-<use> rejection vs clean SVG acceptance
	svgFn, _ := ParseFilename("icon.svg")
	for _, badSVG := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><circle r="5"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body></body></foreignObject></svg>`,
		`<!DOCTYPE svg [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><svg>&xxe;</svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><a href="&#x6a;avascript:alert(1)"><rect width="10" height="10"/></a></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><animate attributeName="href" to="javascript:alert(1)"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><use href="https://evil.example/payload.svg#x"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><style>@import url(https://evil.example/x.css);</style></svg>`,
	} {
		if _, err := ValidateUploadContent(svgFn, "image/svg+xml", []byte(badSVG)); !apperr.IsType(err, apperr.TypeValidation) {
			t.Fatalf("expected malicious SVG rejection for %q, got %v", badSVG, err)
		}
	}

	// UTF-8 multi-byte rune split across 512-byte sniff boundary must still classify as text
	txtFn, _ := ParseFilename("notes.txt")
	longUTF8 := append([]byte(string(make([]byte, 0))), bytes.Repeat([]byte("a"), 510)...)
	longUTF8 = append(longUTF8, []byte("🚀 trailing text")...)
	if ct, err := ValidateUploadContent(txtFn, "text/plain", longUTF8); err != nil || ct != "text/plain; charset=utf-8" {
		t.Fatalf("expected valid UTF-8 text split across 512B boundary, got ct=%q err=%v", ct, err)
	}

	// Metadata bounds & prototype-pollution key rejection
	if err := ValidateMetadata(map[string]any{"__proto__": map[string]any{"admin": true}}); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected prototype pollution metadata rejection, got %v", err)
	}
	deep := map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": "too-deep"}}}}}
	if err := ValidateMetadata(deep); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected deep metadata rejection, got %v", err)
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

func makeTestZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestPDFAndZIPActiveContentValidation(t *testing.T) {
	// Clean minimal PDF passes
	cleanPDF := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n%%EOF\n")
	if err := ValidatePDFContent(cleanPDF); err != nil {
		t.Fatalf("expected clean PDF to pass, got %v", err)
	}

	// Malicious PDFs (JS, hex-obfuscated JS, Launch, OpenAction, EmbeddedFile, XFA, HTML polyglot) must be rejected
	for _, payload := range []string{
		"%PDF-1.7\n1 0 obj << /JavaScript (app.alert(1)) >> endobj",
		"%PDF-1.7\n1 0 obj << /J#61vaScript (app.alert(1)) >> endobj",
		"%PDF-1.7\n1 0 obj << /S /JS /JS (evil) >> endobj",
		"%PDF-1.7\n1 0 obj << /Type /Action /S /Launch /F (cmd.exe) >> endobj",
		"%PDF-1.7\n1 0 obj << /OpenAction 3 0 R >> endobj",
		"%PDF-1.7\n1 0 obj << /AA << /O 4 0 R >> >> endobj",
		"%PDF-1.7\n1 0 obj << /Type /EmbeddedFile >> endobj",
		"%PDF-1.7\n1 0 obj << /XFA 5 0 R >> endobj",
		"%PDF-1.7\n<html><script>alert(1)</script></html>",
	} {
		if err := ValidatePDFContent([]byte(payload)); !apperr.IsType(err, apperr.TypeValidation) {
			t.Fatalf("expected malicious PDF rejection for %q, got %v", payload, err)
		}
	}

	// Clean ZIP archive passes
	cleanZIP := makeTestZIP(t, map[string][]byte{
		"docs/readme.md": []byte("# Hello"),
		"notes.txt":      []byte("safe content"),
	})
	if err := ValidateZIPContent(cleanZIP); err != nil {
		t.Fatalf("expected clean ZIP to pass, got %v", err)
	}

	// Malicious ZIPs: zip-slip traversal, absolute path, embedded executable, double-extension spoof
	for name, entries := range map[string]map[string][]byte{
		"zip-slip":   {"../../etc/cron.d/pwn": []byte("bad")},
		"abs-path":   {"/etc/passwd": []byte("bad")},
		"executable": {"bin/dropper.exe": []byte("MZ")},
		"shell":      {"install.sh": []byte("#!/bin/sh\nid")},
		"double-ext": {"handbook.pdf.js": []byte("alert(1)")},
	} {
		z := makeTestZIP(t, entries)
		if err := ValidateZIPContent(z); !apperr.IsType(err, apperr.TypeValidation) {
			t.Fatalf("expected malicious ZIP (%s) rejection, got %v", name, err)
		}
	}
}
