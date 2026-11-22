package media

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/google/uuid"
)

type SignURLUseCase struct {
	repo       port.AssetRepository
	signer     port.URLSigner
	cdnBaseURL string
}

func NewSignURLUseCase(repo port.AssetRepository, signer port.URLSigner, cdnBaseURL string) *SignURLUseCase {
	return &SignURLUseCase{
		repo:       repo,
		signer:     signer,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

func (uc *SignURLUseCase) Execute(ctx context.Context, in SignURLInput) (*SignURLOutput, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}
	if uc.signer == nil {
		return nil, apperr.Unavailable("URL signing is not configured")
	}

	ttlSecs := in.ExpiresIn
	if ttlSecs == 0 {
		ttlSecs = int64(DefaultSignedURLTTL.Seconds())
	}
	if ttlSecs < MinSignedURLTTLSecs || ttlSecs > MaxSignedURLTTLSecs {
		return nil, apperr.Validation(fmt.Sprintf("expires_in must be between %d and %d seconds", MinSignedURLTTLSecs, MaxSignedURLTTLSecs))
	}

	var key string
	switch {
	case in.AssetID != "":
		if _, err := uuid.Parse(in.AssetID); err != nil {
			return nil, apperr.Validation("invalid asset_id UUID")
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
		key = asset.R2Key
	case in.Key != "":
		key = strings.TrimLeft(strings.TrimSpace(in.Key), "/")
		if in.Actor.Role != "admin" {
			if _, err := model.ValidateOwnedKey(key, in.Actor.UserID); err != nil {
				return nil, err
			}
		} else if strings.Contains(key, "..") || strings.Contains(key, `\`) {
			return nil, apperr.Validation("invalid object key")
		}
	default:
		return nil, apperr.Validation("either asset_id or key is required")
	}

	signedURL, expiresAt := uc.signer.BuildPrivateURL(uc.cdnBaseURL, key, time.Duration(ttlSecs)*time.Second)
	return &SignURLOutput{
		URL:       signedURL,
		Key:       key,
		ExpiresAt: expiresAt,
	}, nil
}

type PurgeCacheUseCase struct {
	purger     port.CachePurger
	cdnBaseURL string
}

func NewPurgeCacheUseCase(purger port.CachePurger, cdnBaseURL string) *PurgeCacheUseCase {
	return &PurgeCacheUseCase{
		purger:     purger,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

func (uc *PurgeCacheUseCase) Execute(ctx context.Context, in PurgeCacheInput) (*PurgeCacheOutput, error) {
	if strings.TrimSpace(in.Actor.UserID) == "" {
		return nil, apperr.Unauthorized("Unauthorized")
	}
	if uc.purger == nil || !uc.purger.Configured() {
		return nil, apperr.Unavailable("Cloudflare cache purge is not configured")
	}
	if len(in.Files) == 0 || len(in.Files) > 30 {
		return nil, apperr.Validation("files must contain between 1 and 30 URLs")
	}

	cdnBase := uc.cdnBaseURL + "/"
	allowedPublic := fmt.Sprintf("%sassets/%s/", cdnBase, in.Actor.UserID)
	allowedPrivate := fmt.Sprintf("%sprivate/%s/", cdnBase, in.Actor.UserID)

	for _, f := range in.Files {
		if !strings.HasPrefix(f, cdnBase) || strings.Contains(f, "..") {
			return nil, apperr.Validation("all purge URLs must belong to the configured CDN domain")
		}
		if in.Actor.Role != "admin" && !strings.HasPrefix(f, allowedPublic) && !strings.HasPrefix(f, allowedPrivate) {
			return nil, apperr.Forbidden("forbidden to purge assets owned by another user")
		}
	}

	if err := uc.purger.PurgeURLs(ctx, in.Files); err != nil {
		return nil, apperr.External("failed to purge CDN cache", err)
	}

	return &PurgeCacheOutput{
		Status: "purged",
		Count:  len(in.Files),
	}, nil
}
