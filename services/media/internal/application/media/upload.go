package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

	effectiveMax := uc.maxUploadBytes
	if filename.Ext == ".svg" && model.MaxSVGBytes < effectiveMax {
		effectiveMax = model.MaxSVGBytes
	}
	if filename.Ext == ".zip" && model.MaxZIPBytes < effectiveMax {
		effectiveMax = model.MaxZIPBytes
	}

	// Read 512 + 1 sentinel byte so DetectFileSignature knows whether a trailing UTF-8 rune was split by truncation.
	sniffBuf := make([]byte, 513)
	n, readErr := io.ReadFull(in.File, sniffBuf)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, apperr.System("failed to read uploaded file", readErr)
	}
	sniffBuf = sniffBuf[:n]

	hash := sha256.New()
	hash.Write(sniffBuf)
	remainingBudget := effectiveMax - int64(n) + 1
	if remainingBudget < 0 {
		remainingBudget = 0
	}
	rest, err := io.Copy(hash, io.LimitReader(in.File, remainingBudget))
	if err != nil {
		return nil, apperr.System("failed to hash uploaded file", err)
	}
	totalSize := int64(n) + rest
	if totalSize == 0 {
		return nil, apperr.Validation("uploaded file is empty")
	}
	if totalSize > effectiveMax {
		if filename.Ext == ".svg" && totalSize > model.MaxSVGBytes {
			return nil, apperr.TooLarge("SVG file exceeds maximum allowed size (2 MiB)")
		}
		if filename.Ext == ".zip" && totalSize > model.MaxZIPBytes {
			return nil, apperr.TooLarge("ZIP archive exceeds maximum inspectable size (32 MiB)")
		}
		return nil, apperr.TooLarge("file exceeds maximum allowed size")
	}

	mimeType, err := model.ValidateUploadContent(filename, in.HeaderContentType, sniffBuf)
	if err != nil {
		return nil, err
	}

	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return nil, apperr.System("failed to rewind uploaded file", err)
	}

	// Scan full SVG body (not just the first 512 bytes) to block scripts/event handlers/XXE anywhere in the document.
	if filename.Ext == ".svg" {
		if totalSize > model.MaxSVGBytes {
			return nil, apperr.TooLarge("SVG file exceeds maximum allowed size (2 MiB)")
		}
		svgBytes, readErr := io.ReadAll(io.LimitReader(in.File, model.MaxSVGBytes+1))
		if readErr != nil {
			return nil, apperr.System("failed to inspect SVG file", readErr)
		}
		if err := model.ValidateSVGContent(svgBytes); err != nil {
			return nil, err
		}
		if _, err := in.File.Seek(0, io.SeekStart); err != nil {
			return nil, apperr.System("failed to rewind uploaded file", err)
		}
	}

	// Scan PDF body (up to 16 MiB window) to reject embedded JS, launch actions, XFA, and polyglots.
	if filename.Ext == ".pdf" {
		pdfBytes, readErr := io.ReadAll(io.LimitReader(in.File, model.MaxPDFScanBytes))
		if readErr != nil {
			return nil, apperr.System("failed to inspect PDF file", readErr)
		}
		if err := model.ValidatePDFContent(pdfBytes); err != nil {
			return nil, err
		}
		if _, err := in.File.Seek(0, io.SeekStart); err != nil {
			return nil, apperr.System("failed to rewind uploaded file", err)
		}
	}

	// Inspect ZIP central directory (up to 32 MiB) to block zip-slip, nested executables, and zip bombs.
	if filename.Ext == ".zip" {
		zipBytes, readErr := io.ReadAll(io.LimitReader(in.File, model.MaxZIPBytes+1))
		if readErr != nil {
			return nil, apperr.System("failed to inspect ZIP archive", readErr)
		}
		if err := model.ValidateZIPContent(zipBytes); err != nil {
			return nil, err
		}
		if _, err := in.File.Seek(0, io.SeekStart); err != nil {
			return nil, apperr.System("failed to rewind uploaded file", err)
		}
	}

	hashHex := hex.EncodeToString(hash.Sum(nil))
	r2Key := model.BuildObjectKey(visibility, in.Actor.UserID, hashHex[:32]+filename.Ext)

	// Record whether this content-addressed key already existed so a transient DB Save
	// failure on a duplicate re-upload never destroys a live, previously confirmed object.
	_, headErr := uc.store.HeadObject(ctx, r2Key)
	existedBefore := headErr == nil

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
		if !existedBefore {
			_ = uc.store.DeleteObject(ctx, r2Key)
		}
		return nil, err
	}

	if uc.repo != nil {
		saved, err := uc.repo.Save(ctx, asset)
		if err != nil {
			if !existedBefore {
				_ = uc.store.DeleteObject(ctx, r2Key)
			}
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
