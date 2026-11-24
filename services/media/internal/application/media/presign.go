package media

import (
	"context"
	"fmt"
	"path/filepath"
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
	contentType, err := model.ValidatePresignContentType(filename, in.ContentType)
	if err != nil {
		return nil, err
	}

	stagingKey := model.BuildStagingKey(visibility, in.Actor.UserID, uuid.NewString()+filename.Ext)
	finalKey := strings.TrimPrefix(stagingKey, "staging/")
	uploadURL, err := uc.store.PresignPutObject(ctx, stagingKey, contentType, DefaultPresignTTL)
	if err != nil {
		return nil, apperr.External("failed to generate presigned upload URL", err)
	}

	return &PresignOutput{
		UploadURL:  uploadURL,
		URL:        uploadURL,
		Key:        stagingKey,
		PublicURL:  fmt.Sprintf("%s/%s", uc.cdnBaseURL, finalKey),
		Visibility: string(visibility),
		ExpiresAt:  time.Now().UTC().Add(DefaultPresignTTL),
	}, nil
}

type ConfirmUploadUseCase struct {
	store          port.ObjectStore
	repo           port.AssetRepository
	signer         port.URLSigner
	bus            event.EventBus
	cdnBaseURL     string
	maxUploadBytes int64
}

func NewConfirmUploadUseCase(
	store port.ObjectStore,
	repo port.AssetRepository,
	signer port.URLSigner,
	bus event.EventBus,
	cdnBaseURL string,
	maxUploadBytes int64,
) *ConfirmUploadUseCase {
	if maxUploadBytes <= 0 {
		maxUploadBytes = 100 << 20
	}
	if bus == nil {
		bus = event.NoopEventBus{}
	}
	return &ConfirmUploadUseCase{
		store:          store,
		repo:           repo,
		signer:         signer,
		bus:            bus,
		cdnBaseURL:     strings.TrimRight(cdnBaseURL, "/"),
		maxUploadBytes: maxUploadBytes,
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
	visibility, finalKey, err := model.ValidateStagingKey(cleanKey, in.Actor.UserID)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(filepath.Ext(cleanKey)) != filename.Ext {
		return nil, apperr.Validation("confirmed filename extension does not match object key extension")
	}
	if err := model.ValidateMetadata(in.Metadata); err != nil {
		return nil, err
	}

	// Single-use presign invariant: once finalKey exists in live storage, re-confirming via a reused
	// presigned URL must be refused with 409 Conflict so immutable CDN assets can never be mutated.
	if _, existsErr := uc.store.HeadObject(ctx, finalKey); existsErr == nil {
		_ = uc.store.DeleteObject(ctx, cleanKey)
		return nil, apperr.Conflict("upload has already been confirmed")
	}

	meta, err := uc.store.HeadObject(ctx, cleanKey)
	if err != nil {
		return nil, apperr.NotFound("uploaded object not found in storage")
	}
	if meta.Size <= 0 {
		_ = uc.store.DeleteObject(ctx, cleanKey)
		return nil, apperr.Validation("uploaded file is empty")
	}
	if meta.Size > uc.maxUploadBytes {
		_ = uc.store.DeleteObject(ctx, cleanKey)
		return nil, apperr.TooLarge("file exceeds maximum allowed size")
	}
	if filename.Ext == ".svg" && meta.Size > model.MaxSVGBytes {
		_ = uc.store.DeleteObject(ctx, cleanKey)
		return nil, apperr.TooLarge("SVG file exceeds maximum allowed size (2 MiB)")
	}

	readLimit := int64(512)
	if filename.Ext == ".svg" {
		readLimit = model.MaxSVGBytes
	}
	// Pin both ReadObjectHead and CopyObject to meta.ETag (S3 IfMatch / CopySourceIfMatch)
	// so concurrent PUTs to the staging key during confirmation abort atomically (zero TOCTOU window).
	headBytes, err := uc.store.ReadObjectHead(ctx, cleanKey, readLimit, meta.ETag)
	if err != nil {
		return nil, apperr.Conflict("uploaded object changed during verification; retry upload")
	}
	mimeType, err := model.ValidateUploadContent(filename, meta.ContentType, headBytes)
	if err != nil {
		_ = uc.store.DeleteObject(ctx, cleanKey)
		return nil, err
	}
	if filename.Ext == ".svg" {
		if err := model.ValidateSVGContent(headBytes); err != nil {
			_ = uc.store.DeleteObject(ctx, cleanKey)
			return nil, err
		}
	}

	// Atomically promote verified staging object into live serving prefix with canonical Content-Type,
	// closing pre-confirm serving, mid-confirm ETag TOCTOU, and post-confirm overwrites.
	promotedETag, err := uc.store.CopyObject(ctx, cleanKey, finalKey, mimeType, meta.ETag)
	if err != nil {
		return nil, apperr.Conflict("uploaded object changed during promotion; retry upload")
	}
	if promotedETag == "" {
		promotedETag = meta.ETag
	}
	_ = uc.store.DeleteObject(ctx, cleanKey)

	publicURL := fmt.Sprintf("%s/%s", uc.cdnBaseURL, finalKey)
	asset, err := model.NewAsset(
		in.Actor.UserID,
		filename,
		mimeType,
		meta.Size,
		finalKey,
		publicURL,
		visibility,
		promotedETag,
		"",
		in.Metadata,
	)
	if err != nil {
		_ = uc.store.DeleteObject(ctx, finalKey)
		return nil, err
	}

	if uc.repo != nil {
		saved, err := uc.repo.Save(ctx, asset)
		if err != nil {
			_ = uc.store.DeleteObject(ctx, finalKey)
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
