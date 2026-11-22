package media

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/media/internal/domain/event"
	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/google/uuid"
)

type PresignUploadUseCase struct {
	store      port.ObjectStore
	cdnBaseURL string
}

func NewPresignUploadUseCase(store port.ObjectStore, cdnBaseURL string) *PresignUploadUseCase {
	return &PresignUploadUseCase{
		store:      store,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

func (uc *PresignUploadUseCase) Execute(ctx context.Context, in PresignInput) (*PresignOutput, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}
	filename, err := model.ParseFilename(in.Filename)
	if err != nil {
		return nil, err
	}
	visibility, err := model.ParseVisibility(in.Visibility)
	if err != nil {
		return nil, err
	}

	key := model.BuildObjectKey(visibility, in.Actor.UserID, uuid.NewString()+filename.Ext)
	uploadURL, err := uc.store.PresignPutObject(ctx, key, in.ContentType, DefaultPresignTTL)
	if err != nil {
		return nil, apperr.External("failed to generate presigned upload URL", err)
	}

	return &PresignOutput{
		UploadURL:  uploadURL,
		URL:        uploadURL,
		Key:        key,
		PublicURL:  fmt.Sprintf("%s/%s", uc.cdnBaseURL, key),
		Visibility: string(visibility),
		ExpiresAt:  time.Now().UTC().Add(DefaultPresignTTL),
	}, nil
}

type ConfirmUploadUseCase struct {
	store      port.ObjectStore
	repo       port.AssetRepository
	signer     port.URLSigner
	bus        event.EventBus
	cdnBaseURL string
}

func NewConfirmUploadUseCase(
	store port.ObjectStore,
	repo port.AssetRepository,
	signer port.URLSigner,
	bus event.EventBus,
	cdnBaseURL string,
) *ConfirmUploadUseCase {
	if bus == nil {
		bus = event.NoopEventBus{}
	}
	return &ConfirmUploadUseCase{
		store:      store,
		repo:       repo,
		signer:     signer,
		bus:        bus,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

func (uc *ConfirmUploadUseCase) Execute(ctx context.Context, in ConfirmUploadInput) (*AssetDTO, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}

	filename, err := model.ParseFilename(in.Filename)
	if err != nil {
		return nil, err
	}
	cleanKey := strings.TrimLeft(strings.TrimSpace(in.Key), "/")
	visibility, err := model.ValidateOwnedKey(cleanKey, in.Actor.UserID)
	if err != nil {
		return nil, err
	}

	meta, err := uc.store.HeadObject(ctx, cleanKey)
	if err != nil {
		return nil, apperr.NotFound("uploaded object not found in storage")
	}

	mimeType := meta.ContentType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	publicURL := fmt.Sprintf("%s/%s", uc.cdnBaseURL, cleanKey)
	asset, err := model.NewAsset(
		in.Actor.UserID,
		filename,
		mimeType,
		meta.Size,
		cleanKey,
		publicURL,
		visibility,
		meta.ETag,
		"",
		in.Metadata,
	)
	if err != nil {
		return nil, err
	}

	if uc.repo != nil {
		saved, err := uc.repo.Save(ctx, asset)
		if err != nil {
			return nil, apperr.System("failed to persist asset metadata", err)
		}
		asset = saved
	} else {
		asset.ID = uuid.NewString()
	}

	asset.MarkUploaded()
	_ = uc.bus.Publish(ctx, asset.PullEvents()...)

	dto := ToAssetDTO(asset, uc.cdnBaseURL, uc.signer)
	return &dto, nil
}
