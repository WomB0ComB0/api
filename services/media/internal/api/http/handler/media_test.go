package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/media/internal/adapter/cloudflare"
	"api/media/internal/api/http/handler"
	"api/media/internal/api/http/middleware"
	"api/media/internal/application"
	"api/media/internal/application/media"
	"api/media/internal/domain/event"
	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type memoryStore struct {
	objects map[string][]byte
	types   map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string][]byte{}, types: map[string]string{}}
}

func (m *memoryStore) PutObject(_ context.Context, key string, body io.Reader, ct string) (string, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	m.objects[key] = b
	m.types[key] = ct
	return "etag-123", nil
}

func (m *memoryStore) HeadObject(_ context.Context, key string) (*port.ObjectMeta, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return &port.ObjectMeta{Size: int64(len(b)), ContentType: m.types[key], ETag: "etag-123"}, nil
}

func (m *memoryStore) DeleteObject(_ context.Context, key string) error {
	delete(m.objects, key)
	delete(m.types, key)
	return nil
}

func (m *memoryStore) ListObjects(_ context.Context, prefix string, _ int32) ([]port.ObjectInfo, error) {
	out := make([]port.ObjectInfo, 0)
	for k, v := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, port.ObjectInfo{Key: k, Size: int64(len(v)), LastModified: time.Now().UTC()})
		}
	}
	return out, nil
}

func (m *memoryStore) PresignPutObject(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "https://r2.example.com/presigned/" + key, nil
}

func (m *memoryStore) HealthCheck(context.Context) error { return nil }

type memoryRepo struct {
	assets map[string]*model.Asset
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{assets: map[string]*model.Asset{}}
}

func (m *memoryRepo) Save(_ context.Context, a *model.Asset) (*model.Asset, error) {
	cp := *a
	if cp.ID == "" {
		cp.ID = uuid.NewString()
	}
	m.assets[cp.ID] = &cp
	return &cp, nil
}

func (m *memoryRepo) FindByID(_ context.Context, id string) (*model.Asset, error) {
	a, ok := m.assets[id]
	if !ok {
		return nil, apperr.NotFound("asset not found")
	}
	cp := *a
	return &cp, nil
}

func (m *memoryRepo) FindByKey(_ context.Context, key string) (*model.Asset, error) {
	for _, a := range m.assets {
		if a.R2Key == key {
			cp := *a
			return &cp, nil
		}
	}
	return nil, apperr.NotFound("asset not found")
}

func (m *memoryRepo) FindByIDs(_ context.Context, ids []string) ([]model.Asset, error) {
	out := make([]model.Asset, 0, len(ids))
	for _, id := range ids {
		if a, ok := m.assets[id]; ok {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memoryRepo) ListByUser(_ context.Context, userID string, _, _ int) ([]model.Asset, int, error) {
	out := make([]model.Asset, 0)
	for _, a := range m.assets {
		if a.UserID == userID {
			out = append(out, *a)
		}
	}
	return out, len(out), nil
}

func (m *memoryRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.assets[id]; !ok {
		return apperr.NotFound("asset not found")
	}
	delete(m.assets, id)
	return nil
}

func (m *memoryRepo) HealthCheck(context.Context) error { return nil }
func (m *memoryRepo) Close()                            {}

type mockPurger struct {
	purged []string
}

func (p *mockPurger) Configured() bool { return true }
func (p *mockPurger) PurgeURLs(_ context.Context, urls []string) error {
	p.purged = append(p.purged, urls...)
	return nil
}

func newTestRouter(factory *application.Factory) *chi.Mux {
	h := handler.NewMediaHandler(factory, 10<<20)
	r := chi.NewRouter()
	r.Get("/v1/media/health", handler.HealthCheck)
	r.Get("/v1/media/health/ready", handler.ReadyCheck(factory.ObjectStore(), factory.AssetRepository()))
	r.Get("/v1/media/openapi.json", handler.OpenAPISpec)
	r.Post("/v1/media/upload", h.Upload)
	r.Post("/v1/media/presign", h.PresignUpload)
	r.Post("/v1/media/assets/confirm", h.ConfirmUpload)
	r.Post("/v1/media/sign", h.SignURL)
	r.Post("/v1/media/purge", h.PurgeCache)
	r.Get("/v1/media/assets", h.ListAssets)
	r.Get("/v1/media/assets/{id}", h.GetAsset)
	r.Delete("/v1/media/assets/{id}", h.DeleteAsset)
	return r
}

func TestHTTPMediaLifecycleAndEventDrivenPurge(t *testing.T) {
	store := newMemoryStore()
	repo := newMemoryRepo()
	purger := &mockPurger{}
	signer := cloudflare.NewURLSigner("test-signing-secret-32-bytes-long!!")
	bus := event.NewInMemoryEventBus()
	bus.Subscribe(cloudflare.NewAssetDeletedPurgeSubscriber(purger))

	factory := application.NewFactory(store, repo, signer, purger, bus, "https://cdn.mikeodnis.dev", 10<<20)
	router := newTestRouter(factory)

	user1 := uuid.NewString()
	user2 := uuid.NewString()

	// Disallowed extension -> 400
	var badBody bytes.Buffer
	mw := multipart.NewWriter(&badBody)
	part, _ := mw.CreateFormFile("file", "malware.exe")
	_, _ = part.Write([]byte("MZ"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/media/upload", &badBody)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(middleware.WithUser(req.Context(), user1, "user"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// Upload public PNG -> 201
	var pubBody bytes.Buffer
	mw = multipart.NewWriter(&pubBody)
	part, _ = mw.CreateFormFile("file", "avatar.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nfake-png-content"))
	_ = mw.Close()

	req = httptest.NewRequest(http.MethodPost, "/v1/media/upload", &pubBody)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(middleware.WithUser(req.Context(), user1, "user"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var uploaded media.AssetDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !strings.HasPrefix(uploaded.Key, "assets/"+user1+"/") {
		t.Fatalf("unexpected key: %s", uploaded.Key)
	}

	// Cross-user GET -> 403
	req = httptest.NewRequest(http.MethodGet, "/v1/media/assets/"+uploaded.ID, nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user2, "user"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// Upload private PDF -> signed CDN URL
	var privBody bytes.Buffer
	mw = multipart.NewWriter(&privBody)
	_ = mw.WriteField("visibility", "private")
	part, _ = mw.CreateFormFile("file", "invoice.pdf")
	_, _ = part.Write([]byte("%PDF-1.7\nsecret"))
	_ = mw.Close()

	req = httptest.NewRequest(http.MethodPost, "/v1/media/upload", &privBody)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(middleware.WithUser(req.Context(), user1, "user"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for private PDF, got %d: %s", rec.Code, rec.Body.String())
	}
	var privUploaded media.AssetDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &privUploaded)
	if !strings.HasPrefix(privUploaded.Key, "private/"+user1+"/") || !strings.Contains(privUploaded.URL, "?exp=") {
		t.Fatalf("expected signed private URL, got %+v", privUploaded)
	}

	// Delete public asset -> triggers AssetDeletedEvent -> CloudflarePurgeSubscriber purges CDN URL!
	req = httptest.NewRequest(http.MethodDelete, "/v1/media/assets/"+uploaded.ID, nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user1, "user"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(purger.purged) != 1 || purger.purged[0] != uploaded.URL {
		t.Fatalf("expected event-driven CDN purge for %s, got %v", uploaded.URL, purger.purged)
	}
}

func TestHTTPPresignAndConfirm(t *testing.T) {
	store := newMemoryStore()
	repo := newMemoryRepo()
	factory := application.NewFactory(store, repo, nil, nil, nil, "https://cdn.mikeodnis.dev", 10<<20)
	router := newTestRouter(factory)
	user1 := uuid.NewString()

	req := httptest.NewRequest(http.MethodPost, "/v1/media/presign", strings.NewReader(`{"filename":"banner.webp","content_type":"image/webp"}`))
	req = req.WithContext(middleware.WithUser(req.Context(), user1, "user"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var pre media.PresignOutput
	_ = json.Unmarshal(rec.Body.Bytes(), &pre)
	store.objects[pre.Key] = []byte("RIFF....WEBP")
	store.types[pre.Key] = "image/webp"

	req = httptest.NewRequest(http.MethodPost, "/v1/media/assets/confirm", strings.NewReader(`{"key":"`+pre.Key+`","filename":"banner.webp"}`))
	req = req.WithContext(middleware.WithUser(req.Context(), user1, "user"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}
