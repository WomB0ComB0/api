package media_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"api/media/internal/application"
	"api/media/internal/application/media"
	"api/media/internal/domain/event"
	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/google/uuid"
)

type memStore struct {
	objects         map[string][]byte
	types           map[string]string
	etags           map[string]string
	mutateOnReadKey string // simulates concurrent TOCTOU PUT during ConfirmUpload
}

func (m *memStore) etagOf(key string) string {
	if m.etags != nil && m.etags[key] != "" {
		return m.etags[key]
	}
	return "etag-test"
}

func (m *memStore) PutObject(_ context.Context, key string, body io.Reader, ct string) (string, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	m.objects[key] = b
	m.types[key] = ct
	return "etag-test", nil
}

func (m *memStore) CopyObject(_ context.Context, srcKey, dstKey, ct, expectedETag string) (string, error) {
	b, ok := m.objects[srcKey]
	if !ok {
		return "", errors.New("not found")
	}
	if expectedETag != "" && m.etagOf(srcKey) != expectedETag {
		return "", errors.New("precondition failed: etag mismatch")
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	m.objects[dstKey] = cp
	m.types[dstKey] = ct
	return "etag-promoted", nil
}

func (m *memStore) HeadObject(_ context.Context, key string) (*port.ObjectMeta, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return &port.ObjectMeta{Size: int64(len(b)), ContentType: m.types[key], ETag: m.etagOf(key)}, nil
}

func (m *memStore) ReadObjectHead(_ context.Context, key string, maxBytes int64, expectedETag string) ([]byte, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	if expectedETag != "" && m.etagOf(key) != expectedETag {
		return nil, errors.New("precondition failed: etag mismatch")
	}
	out := b
	if int64(len(out)) > maxBytes {
		out = out[:maxBytes]
	}
	// If test arming a TOCTOU race, mutate the staging object's ETag right after head read (before CopyObject!)
	if m.mutateOnReadKey == key {
		if m.etags == nil {
			m.etags = map[string]string{}
		}
		m.etags[key] = "etag-mutated-by-attacker"
		m.objects[key] = []byte("<html>TOCTOU-race-payload</html>")
	}
	return out, nil
}

func (m *memStore) DeleteObject(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func (m *memStore) ListObjects(_ context.Context, prefix string, _ int32) ([]port.ObjectInfo, error) {
	out := make([]port.ObjectInfo, 0)
	for k, v := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, port.ObjectInfo{Key: k, Size: int64(len(v)), LastModified: time.Now().UTC()})
		}
	}
	return out, nil
}

func (m *memStore) PresignPutObject(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "https://r2.local/" + key, nil
}

func (m *memStore) HealthCheck(context.Context) error { return nil }

type memRepo struct {
	assets  map[string]*model.Asset
	queries int
}

func (m *memRepo) Save(_ context.Context, a *model.Asset) (*model.Asset, error) {
	m.queries++
	cp := *a
	if cp.ID == "" {
		cp.ID = uuid.NewString()
	}
	m.assets[cp.ID] = &cp
	return &cp, nil
}

func (m *memRepo) FindByID(_ context.Context, id string) (*model.Asset, error) {
	m.queries++
	a, ok := m.assets[id]
	if !ok {
		return nil, apperr.NotFound("asset not found")
	}
	cp := *a
	return &cp, nil
}

func (m *memRepo) FindByIDs(_ context.Context, ids []string) ([]model.Asset, error) {
	m.queries++
	out := make([]model.Asset, 0, len(ids))
	for _, id := range ids {
		if a, ok := m.assets[id]; ok {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memRepo) FindByKey(_ context.Context, key string) (*model.Asset, error) {
	m.queries++
	for _, a := range m.assets {
		if a.R2Key == key {
			cp := *a
			return &cp, nil
		}
	}
	return nil, apperr.NotFound("asset not found")
}

func (m *memRepo) ListByUser(_ context.Context, userID string, limit, offset int) ([]model.Asset, int, error) {
	m.queries++
	all := make([]model.Asset, 0)
	for _, a := range m.assets {
		if a.UserID == userID {
			all = append(all, *a)
		}
	}
	total := len(all)
	if offset >= total {
		return []model.Asset{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (m *memRepo) Delete(_ context.Context, id string) error {
	m.queries++
	if _, ok := m.assets[id]; !ok {
		return apperr.NotFound("asset not found")
	}
	delete(m.assets, id)
	return nil
}

func (m *memRepo) HealthCheck(context.Context) error { return nil }
func (m *memRepo) Close()                            {}

type captureEventHandler struct {
	events []event.Event
}

func (h *captureEventHandler) InterestedIn(string) bool { return true }
func (h *captureEventHandler) Handle(_ context.Context, evt event.Event) error {
	h.events = append(h.events, evt)
	return nil
}

func TestApplicationUseCasesAndDomainEvents(t *testing.T) {
	store := &memStore{objects: map[string][]byte{}, types: map[string]string{}}
	repo := &memRepo{assets: map[string]*model.Asset{}}
	bus := event.NewInMemoryEventBus()
	listener := &captureEventHandler{}
	bus.Subscribe(listener)

	factory := application.NewFactory(store, repo, nil, nil, bus, "https://cdn.mikeodnis.dev", 10<<20)

	u1 := media.Actor{UserID: uuid.NewString(), Role: "user"}
	u2 := media.Actor{UserID: uuid.NewString(), Role: "user"}

	// Upload public asset
	uploaded, err := factory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:    u1,
		Filename: "photo.png",
		File:     bytes.NewReader([]byte("\x89PNG\r\n\x1a\npayload")),
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if !strings.HasPrefix(uploaded.Key, "assets/"+u1.UserID+"/") {
		t.Fatalf("unexpected R2 key: %s", uploaded.Key)
	}

	// Cross-user get rejected with FORBIDDEN
	if _, err := factory.GetAsset().Execute(context.Background(), media.GetAssetInput{
		Actor:   u2,
		AssetID: uploaded.ID,
	}); !apperr.IsType(err, apperr.TypeForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}

	// Delete publishes AssetDeletedEvent
	if err := factory.DeleteAsset().Execute(context.Background(), media.DeleteAssetInput{
		Actor:   u1,
		AssetID: uploaded.ID,
	}); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if len(listener.events) != 2 ||
		listener.events[0].EventName() != event.EventAssetUploaded ||
		listener.events[1].EventName() != event.EventAssetDeleted {
		t.Fatalf("expected uploaded + deleted domain events, got %+v", listener.events)
	}
}

func TestListAssetsIsNPlusOneFree(t *testing.T) {
	store := &memStore{objects: map[string][]byte{}, types: map[string]string{}}
	repo := &memRepo{assets: map[string]*model.Asset{}}
	factory := application.NewFactory(store, repo, nil, nil, nil, "https://cdn.mikeodnis.dev", 10<<20)

	u1 := media.Actor{UserID: uuid.NewString(), Role: "user"}
	ids := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		out, err := factory.Upload().Execute(context.Background(), media.UploadInput{
			Actor:    u1,
			Filename: "img.png",
			File:     bytes.NewReader([]byte("\x89PNG\r\n\x1a\n" + uuid.NewString())),
		})
		if err != nil {
			t.Fatalf("seed upload %d failed: %v", i, err)
		}
		ids = append(ids, out.ID)
	}

	// Listing 20 assets with page size 10 must execute exactly 1 DB query (no N+1, no extra COUNT query)
	repo.queries = 0
	page, err := factory.ListAssets().Execute(context.Background(), media.ListAssetsInput{
		Actor:  u1,
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(page.Assets) != 10 || page.Total != 20 {
		t.Fatalf("unexpected pagination: len=%d total=%d", len(page.Assets), page.Total)
	}
	if repo.queries != 1 {
		t.Fatalf("expected exactly 1 query for ListAssets (N+1-free), got %d", repo.queries)
	}

	// Batch lookup of 20 IDs must execute exactly 1 query
	repo.queries = 0
	batched, err := repo.FindByIDs(context.Background(), ids)
	if err != nil || len(batched) != 20 || repo.queries != 1 {
		t.Fatalf("expected 20 batched assets in 1 query, got len=%d queries=%d err=%v", len(batched), repo.queries, err)
	}
}

func TestUploadAndConfirmSecurityEnforcement(t *testing.T) {
	store := &memStore{objects: map[string][]byte{}, types: map[string]string{}}
	repo := &memRepo{assets: map[string]*model.Asset{}}
	factory := application.NewFactory(store, repo, nil, nil, nil, "https://cdn.mikeodnis.dev", 1024)
	u1 := media.Actor{UserID: uuid.NewString(), Role: "user"}

	// 1. Late-body SVG <script> past byte 512 must be rejected before R2 PutObject
	padding := strings.Repeat(" ", 600)
	evilSVG := `<svg xmlns="http://www.w3.org/2000/svg">` + padding + `<script>alert(1)</script></svg>`
	if _, err := factory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:             u1,
		Filename:          "logo.svg",
		HeaderContentType: "image/svg+xml",
		File:              bytes.NewReader([]byte(evilSVG)),
	}); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected late-body SVG script rejection, got %v", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("rejected SVG must not be stored in R2")
	}

	// 2. Presign confirm with spoofed bytes must reject AND delete the object from R2
	pre, err := factory.PresignUpload().Execute(context.Background(), media.PresignInput{
		Actor:       u1,
		Filename:    "photo.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatalf("presign failed: %v", err)
	}
	store.objects[pre.Key] = []byte("<html><script>evil()</script></html>")
	store.types[pre.Key] = "image/png"
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      pre.Key,
		Filename: "photo.png",
	}); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected spoofed presign confirm rejection, got %v", err)
	}
	if _, exists := store.objects[pre.Key]; exists {
		t.Fatalf("spoofed presigned object must be purged from R2 on failed confirm")
	}

	// 3. Presign confirm exceeding maxUploadBytes must reject (413) AND delete from R2
	store.objects[pre.Key] = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 2048)...)
	store.types[pre.Key] = "image/png"
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      pre.Key,
		Filename: "photo.png",
	}); !apperr.IsType(err, apperr.TypeTooLarge) {
		t.Fatalf("expected oversized presign confirm rejection (413), got %v", err)
	}
	if _, exists := store.objects[pre.Key]; exists {
		t.Fatalf("oversized presigned object must be purged from R2 on failed confirm")
	}

	// 4. Presign confirm with mismatched filename extension vs key extension must reject
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      pre.Key, // ends in .png
		Filename: "spoofed.txt",
	}); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected key-extension mismatch rejection, got %v", err)
	}

	// 5. PurgeCache must reject percent-encoded traversal targeting another user's assets
	purger := &noopPurger{}
	purgeFactory := application.NewFactory(store, repo, nil, purger, nil, "https://cdn.mikeodnis.dev", 1024)
	if _, err := purgeFactory.PurgeCache().Execute(context.Background(), media.PurgeCacheInput{
		Actor: u1,
		Files: []string{"https://cdn.mikeodnis.dev/assets/" + u1.UserID + "/%2e%2e/other-user/secret.png"},
	}); err == nil {
		t.Fatalf("expected percent-encoded purge traversal to be rejected")
	}

	// 6. Nemesis Finding 1 (Staging -> Promote): presigned PUT writes to staging/*, Confirm promotes to assets/*,
	// deletes staging/*, prevents replay, and post-confirm overwrite of staging/* cannot mutate live assets/*!
	if !strings.HasPrefix(pre.Key, "staging/assets/"+u1.UserID+"/") {
		t.Fatalf("expected staging presign key, got %s", pre.Key)
	}
	validPNG := []byte("\x89PNG\r\n\x1a\nvalid-png-data")
	store.objects[pre.Key] = validPNG
	store.types[pre.Key] = "image/png"
	confirmed, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      pre.Key,
		Filename: "photo.png",
	})
	if err != nil {
		t.Fatalf("expected valid staging confirm to succeed, got %v", err)
	}
	if strings.HasPrefix(confirmed.Key, "staging/") || !strings.HasPrefix(confirmed.Key, "assets/"+u1.UserID+"/") {
		t.Fatalf("expected promoted live key, got %s", confirmed.Key)
	}
	if _, stillInStaging := store.objects[pre.Key]; stillInStaging {
		t.Fatalf("staging object must be removed after promotion")
	}
	// Replaying confirm on already-promoted staging key must fail with Conflict (single-use invariant!)
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      pre.Key,
		Filename: "photo.png",
	}); !apperr.IsType(err, apperr.TypeConflict) {
		t.Fatalf("expected replay confirm on already-promoted key to return Conflict, got %v", err)
	}
	// Post-confirm TOCTOU: reusing the presigned URL to write a NEW valid PNG and re-confirming must fail with Conflict
	// and purge staging/* while leaving the original live immutable asset untouched!
	store.objects[pre.Key] = []byte("\x89PNG\r\n\x1a\nsecond-png-attempt")
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      pre.Key,
		Filename: "photo.png",
	}); !apperr.IsType(err, apperr.TypeConflict) {
		t.Fatalf("expected second confirm after staging overwrite to return Conflict, got %v", err)
	}
	if !bytes.Equal(store.objects[confirmed.Key], validPNG) {
		t.Fatalf("live confirmed asset was mutated by post-confirm staging overwrite")
	}
	if _, stillInStaging := store.objects[pre.Key]; stillInStaging {
		t.Fatalf("reused staging object must be deleted on Conflict")
	}

	// 6b. Mid-confirm ETag TOCTOU race: attacker swaps staging object between ReadObjectHead and CopyObject -> must abort (409)!
	preRace, err := factory.PresignUpload().Execute(context.Background(), media.PresignInput{
		Actor:       u1,
		Filename:    "race.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatalf("presign race failed: %v", err)
	}
	store.objects[preRace.Key] = validPNG
	store.types[preRace.Key] = "image/png"
	store.mutateOnReadKey = preRace.Key
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      preRace.Key,
		Filename: "race.png",
	}); !apperr.IsType(err, apperr.TypeConflict) {
		t.Fatalf("expected mid-confirm ETag TOCTOU race to fail with Conflict, got %v", err)
	}
	store.mutateOnReadKey = ""
	liveRaceKey := strings.TrimPrefix(preRace.Key, "staging/")
	if _, leaked := store.objects[liveRaceKey]; leaked {
		t.Fatalf("TOCTOU-mutated object must NEVER be promoted to live prefix")
	}

	// 7. Nemesis Finding 2: transient DB Save error on duplicate re-upload must NOT delete pre-existing live R2 object!
	failRepo := &failingSaveRepo{AssetRepository: repo}
	liveAsset, err := factory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:    u1,
		Filename: "keep-me.png",
		File:     bytes.NewReader(validPNG),
	})
	if err != nil {
		t.Fatalf("initial upload failed: %v", err)
	}
	failFactory := application.NewFactory(store, failRepo, nil, nil, nil, "https://cdn.mikeodnis.dev", 1024)
	if _, err := failFactory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:    u1,
		Filename: "keep-me.png",
		File:     bytes.NewReader(validPNG),
	}); err == nil {
		t.Fatalf("expected DB save failure")
	}
	if _, stillLive := store.objects[liveAsset.Key]; !stillLive {
		t.Fatalf("transient DB failure on duplicate re-upload destroyed pre-existing live R2 object!")
	}

	// 8. Nemesis Round 7: UploadUseCase & ConfirmUploadUseCase must accept valid >512B UTF-8 text and SVG
	// whose multi-byte UTF-8 rune straddles byte 511-512 (proving 513-byte sentinel sniff works).
	splitUTF8Txt := append(bytes.Repeat([]byte("a"), 510), []byte("🚀 trailing text")...)
	if _, err := factory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:             u1,
		Filename:          "notes.txt",
		HeaderContentType: "text/plain",
		File:              bytes.NewReader(splitUTF8Txt),
	}); err != nil {
		t.Fatalf("expected >512B UTF-8 text with split rune at byte 512 to upload cleanly, got %v", err)
	}
	svgHead := `<svg xmlns="http://www.w3.org/2000/svg"><desc>`
	splitUTF8SVG := []byte(svgHead + strings.Repeat("a", 510-len(svgHead)) + "🚀</desc></svg>")
	if _, err := factory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:             u1,
		Filename:          "emoji.svg",
		HeaderContentType: "image/svg+xml",
		File:              bytes.NewReader(splitUTF8SVG),
	}); err != nil {
		t.Fatalf("expected >512B UTF-8 SVG with split rune at byte 512 to upload cleanly, got %v", err)
	}
	preTxt, err := factory.PresignUpload().Execute(context.Background(), media.PresignInput{
		Actor:       u1,
		Filename:    "notes.txt",
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("presign txt failed: %v", err)
	}
	store.objects[preTxt.Key] = splitUTF8Txt
	store.types[preTxt.Key] = "text/plain"
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      preTxt.Key,
		Filename: "notes.txt",
	}); err != nil {
		t.Fatalf("expected ConfirmUpload of >512B UTF-8 text with split rune at byte 512 to succeed, got %v", err)
	}

	// 9. Nemesis Round 7: SignURL must reject query-injected / empty-leaf keys for both user and admin
	signer := &stubSigner{}
	signFactory := application.NewFactory(store, repo, signer, nil, nil, "https://cdn.mikeodnis.dev", 1024)
	admin := media.Actor{UserID: uuid.NewString(), Role: "admin"}
	for _, actor := range []media.Actor{u1, admin} {
		for _, badKey := range []string{
			"private/" + actor.UserID + "/",
			"private/" + actor.UserID + "/doc.pdf?exp=9999999999",
			"private/" + actor.UserID + "/doc.pdf#frag",
		} {
			if _, err := signFactory.SignURL().Execute(context.Background(), media.SignURLInput{
				Actor: actor,
				Key:   badKey,
			}); !apperr.IsType(err, apperr.TypeValidation) {
				t.Fatalf("expected SignURL rejection for actor=%s badKey=%q, got %v", actor.Role, badKey, err)
			}
		}
	}

	// 10. Weaponized PDF (direct upload & presign confirm) must be rejected and purged from staging
	evilPDF := []byte("%PDF-1.7\n1 0 obj << /OpenAction << /S /J#61vaScript /JS (app.alert(1)) >> >> endobj\n%%EOF\n")
	if _, err := factory.Upload().Execute(context.Background(), media.UploadInput{
		Actor:             u1,
		Filename:          "book.pdf",
		HeaderContentType: "application/pdf",
		File:              bytes.NewReader(evilPDF),
	}); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected weaponized PDF direct upload rejection, got %v", err)
	}
	prePDF, err := factory.PresignUpload().Execute(context.Background(), media.PresignInput{
		Actor:       u1,
		Filename:    "book.pdf",
		ContentType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("presign pdf failed: %v", err)
	}
	store.objects[prePDF.Key] = evilPDF
	store.types[prePDF.Key] = "application/pdf"
	if _, err := factory.ConfirmUpload().Execute(context.Background(), media.ConfirmUploadInput{
		Actor:    u1,
		Key:      prePDF.Key,
		Filename: "book.pdf",
	}); !apperr.IsType(err, apperr.TypeValidation) {
		t.Fatalf("expected weaponized PDF presign confirm rejection, got %v", err)
	}
	if _, stillInStaging := store.objects[prePDF.Key]; stillInStaging {
		t.Fatalf("rejected PDF must be purged from staging R2")
	}
}

type stubSigner struct{}

func (*stubSigner) SignPath(string, time.Time) string { return "sig" }
func (*stubSigner) BuildPrivateURL(base, key string, ttl time.Duration) (string, time.Time) {
	return base + "/" + key + "?exp=1&sig=sig", time.Now().Add(ttl)
}
func (*stubSigner) Verify(string, string, string, time.Time) bool { return true }

type failingSaveRepo struct {
	port.AssetRepository
}

func (*failingSaveRepo) Save(context.Context, *model.Asset) (*model.Asset, error) {
	return nil, errors.New("transient postgres timeout")
}

type noopPurger struct{}

func (*noopPurger) Configured() bool                             { return true }
func (*noopPurger) PurgeURLs(context.Context, []string) error    { return nil }
