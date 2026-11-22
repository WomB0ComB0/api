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
	objects map[string][]byte
	types   map[string]string
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

func (m *memStore) HeadObject(_ context.Context, key string) (*port.ObjectMeta, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return &port.ObjectMeta{Size: int64(len(b)), ContentType: m.types[key], ETag: "etag-test"}, nil
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
