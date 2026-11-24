package media

import (
	"context"
	"fmt"
	"strings"

	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/google/uuid"
)

type GetAssetUseCase struct {
	repo       port.AssetRepository
	signer     port.URLSigner
	cdnBaseURL string
}

func NewGetAssetUseCase(repo port.AssetRepository, signer port.URLSigner, cdnBaseURL string) *GetAssetUseCase {
	return &GetAssetUseCase{
		repo:       repo,
		signer:     signer,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

func (uc *GetAssetUseCase) Execute(ctx context.Context, in GetAssetInput) (*AssetDTO, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}
	if _, err := uuid.Parse(in.AssetID); err != nil {
		return nil, apperr.Validation("invalid asset ID UUID")
	}
	if uc.repo == nil {
		return nil, apperr.Unavailable("asset database is not configured")
	}

	asset, err := uc.repo.FindByID(ctx, in.AssetID)
	if err != nil {
		return nil, err
	}
	if !asset.CanAccess(in.Actor.UserID, in.Actor.Role) {
		return nil, apperr.Forbidden("Forbidden")
	}

	dto := ToAssetDTO(asset, uc.cdnBaseURL, uc.signer)
	return &dto, nil
}

type ListAssetsUseCase struct {
	store      port.ObjectStore
	repo       port.AssetRepository
	signer     port.URLSigner
	cdnBaseURL string
}

func NewListAssetsUseCase(
	store port.ObjectStore,
	repo port.AssetRepository,
	signer port.URLSigner,
	cdnBaseURL string,
) *ListAssetsUseCase {
	return &ListAssetsUseCase{
		store:      store,
		repo:       repo,
		signer:     signer,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

func (uc *ListAssetsUseCase) Execute(ctx context.Context, in ListAssetsInput) (*ListAssetsOutput, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > 10_000 {
		return nil, apperr.Validation("offset must not exceed 10000")
	}

	if uc.repo != nil {
		assets, total, err := uc.repo.ListByUser(ctx, in.Actor.UserID, limit, offset)
		if err != nil {
			return nil, apperr.System("failed to list assets", err)
		}
		items := make([]AssetDTO, 0, len(assets))
		for i := range assets {
			items = append(items, ToAssetDTO(&assets[i], uc.cdnBaseURL, uc.signer))
		}
		return &ListAssetsOutput{Assets: items, Total: total, Limit: limit, Offset: offset}, nil
	}

	objs, err := uc.store.ListObjects(ctx, fmt.Sprintf("assets/%s/", in.Actor.UserID), int32(limit))
	if err != nil {
		return nil, apperr.External("failed to list objects from storage", err)
	}
	items := make([]AssetDTO, 0, len(objs))
	for _, obj := range objs {
		items = append(items, AssetDTO{
			Key:        obj.Key,
			URL:        fmt.Sprintf("%s/%s", uc.cdnBaseURL, obj.Key),
			Visibility: string(model.VisibilityPublic),
			Size:       obj.Size,
			ETag:       obj.ETag,
			CreatedAt:  obj.LastModified,
		})
	}
	return &ListAssetsOutput{Assets: items, Total: len(items), Limit: limit, Offset: offset}, nil
}
