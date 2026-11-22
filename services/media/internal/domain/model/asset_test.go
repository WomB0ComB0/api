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
