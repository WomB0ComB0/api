package port

import (
	"context"

	"api/media/internal/domain/model"
)

// AssetRepository is the outbound persistence port for media assets.
type AssetRepository interface {
	Save(ctx context.Context, asset *model.Asset) (*model.Asset, error)
	FindByID(ctx context.Context, id string) (*model.Asset, error)
	// FindByIDs fetches multiple assets in a single batched query (WHERE id = ANY($1::uuid[])) to prevent N+1 lookups.
	FindByIDs(ctx context.Context, ids []string) ([]model.Asset, error)
	FindByKey(ctx context.Context, key string) (*model.Asset, error)
	// ListByUser returns a page of assets and the total count in a single query using COUNT(*) OVER().
	ListByUser(ctx context.Context, userID string, limit, offset int) (assets []model.Asset, total int, err error)
	Delete(ctx context.Context, id string) error
	HealthCheck(ctx context.Context) error
	Close()
}
