package media

import (
	"context"
	"strings"

	"api/media/internal/domain/event"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/google/uuid"
)

type DeleteAssetUseCase struct {
	store port.ObjectStore
	repo  port.AssetRepository
	bus   event.EventBus
}

func NewDeleteAssetUseCase(store port.ObjectStore, repo port.AssetRepository, bus event.EventBus) *DeleteAssetUseCase {
	if bus == nil {
		bus = event.NoopEventBus{}
	}
	return &DeleteAssetUseCase{
		store: store,
		repo:  repo,
		bus:   bus,
	}
}

func (uc *DeleteAssetUseCase) Execute(ctx context.Context, in DeleteAssetInput) error {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return apperr.Unauthorized("Unauthorized")
	}
	if _, err := uuid.Parse(in.AssetID); err != nil {
		return apperr.Validation("invalid asset ID UUID")
	}
	if uc.repo == nil {
		return apperr.Unavailable("asset database is not configured")
	}

	asset, err := uc.repo.FindByID(ctx, in.AssetID)
	if err != nil {
		return err
	}
	if !asset.CanAccess(in.Actor.UserID, in.Actor.Role) {
		return apperr.Forbidden("Forbidden")
	}

	if err := uc.store.DeleteObject(ctx, asset.R2Key); err != nil {
		return apperr.External("failed to delete object from storage", err)
	}
	if err := uc.repo.Delete(ctx, asset.ID); err != nil && !apperr.IsType(err, apperr.TypeNotFound) {
		return apperr.System("failed to delete asset metadata", err)
	}

	// Emit domain event (Cloudflare purge subscriber reacts asynchronously/decoupled)
	asset.MarkDeleted()
	_ = uc.bus.Publish(ctx, asset.PullEvents()...)
	return nil
}
