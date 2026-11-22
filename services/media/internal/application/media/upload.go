package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"api/media/internal/domain/event"
	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"
)

type UploadUseCase struct {
	store          port.ObjectStore
	repo           port.AssetRepository
	signer         port.URLSigner
	bus            event.EventBus
	cdnBaseURL     string
	maxUploadBytes int64
}

func NewUploadUseCase(
	store port.ObjectStore,
	repo port.AssetRepository,
	signer port.URLSigner,
	bus event.EventBus,
	cdnBaseURL string,
	maxUploadBytes int64,
) *UploadUseCase {
	if maxUploadBytes <= 0 {
		maxUploadBytes = 100 << 20
	}
	if bus == nil {
		bus = event.NoopEventBus{}
	}
	return &UploadUseCase{
		store:          store,
		repo:           repo,
		signer:         signer,
		bus:            bus,
		cdnBaseURL:     strings.TrimRight(cdnBaseURL, "/"),
		maxUploadBytes: maxUploadBytes,
	}
}

func (uc *UploadUseCase) Execute(ctx context.Context, in UploadInput) (*AssetDTO, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}
	if in.File == nil {
		return nil, apperr.Validation("file is required")
	}

	filename, err := model.ParseFilename(in.Filename)
	if err != nil {
		return nil, err
	}
	visibility, err := model.ParseVisibility(in.Visibility)
	if err != nil {
		return nil, err
	}

	sniffBuf := make([]byte, 512)
	n, readErr := io.ReadFull(in.File, sniffBuf)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, apperr.System("failed to read uploaded file", readErr)
	}
	sniffBuf = sniffBuf[:n]

	hash := sha256.New()
	hash.Write(sniffBuf)
	rest, err := io.Copy(hash, io.LimitReader(in.File, uc.maxUploadBytes-int64(n)+1))
	if err != nil {
		return nil, apperr.System("failed to hash uploaded file", err)
	}
	totalSize := int64(n) + rest
	if totalSize > uc.maxUploadBytes {
		return nil, apperr.TooLarge("file exceeds maximum allowed size")
	}

	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return nil, apperr.System("failed to rewind uploaded file", err)
	}

	hashHex := hex.EncodeToString(hash.Sum(nil))
	mimeType := resolveContentType(in.HeaderContentType, sniffBuf, filename.Ext)
	r2Key := model.BuildObjectKey(visibility, in.Actor.UserID, hashHex[:32]+filename.Ext)

	etag, err := uc.store.PutObject(ctx, r2Key, in.File, mimeType)
	if err != nil {
		return nil, apperr.External("failed to upload object to storage", err)
	}

	publicURL := fmt.Sprintf("%s/%s", uc.cdnBaseURL, r2Key)
	asset, err := model.NewAsset(
		in.Actor.UserID,
		filename,
		mimeType,
		totalSize,
		r2Key,
		publicURL,
		visibility,
		etag,
		hashHex,
		map[string]any{},
	)
	if err != nil {
		_ = uc.store.DeleteObject(ctx, r2Key)
		return nil, err
	}

	if uc.repo != nil {
		saved, err := uc.repo.Save(ctx, asset)
		if err != nil {
			_ = uc.store.DeleteObject(ctx, r2Key)
			return nil, apperr.System("failed to persist asset metadata", err)
		}
		asset = saved
	} else {
		asset.ID = hashHex[:32]
	}

	asset.MarkUploaded()
	_ = uc.bus.Publish(ctx, asset.PullEvents()...)

	dto := ToAssetDTO(asset, uc.cdnBaseURL, uc.signer)
	return &dto, nil
}

func resolveContentType(headerType string, sniff []byte, ext string) string {
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	}

	detected := http.DetectContentType(sniff)
	if detected != "" && detected != "application/octet-stream" {
		if strings.HasPrefix(detected, "text/plain") {
			switch ext {
			case ".json":
				return "application/json"
			case ".csv":
				return "text/csv"
			case ".md":
				return "text/markdown"
			}
		}
		return detected
	}
	if headerType != "" {
		return headerType
	}
	return "application/octet-stream"
}
