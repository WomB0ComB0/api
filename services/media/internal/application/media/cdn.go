package media

import (
	"context"
	"fmt"
	"net/url"
	"path"
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
		if asset.Visibility != model.VisibilityPrivate {
			return nil, apperr.Validation("only private assets require signed URLs")
		}
		key = asset.R2Key
	case in.Key != "":
		key = strings.TrimLeft(strings.TrimSpace(in.Key), "/")
		if !strings.HasPrefix(key, "private/") {
			return nil, apperr.Validation("only private/* keys can be signed")
		}
		if in.Actor.Role != "admin" {
			if _, err := model.ValidateOwnedKey(key, in.Actor.UserID); err != nil {
				return nil, err
			}
		} else {
			if err := model.ValidateObjectKeySafety(key); err != nil {
				return nil, err
			}
			// Require at least private/<owner>/<leaf>
			rest := strings.TrimPrefix(key, "private/")
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, apperr.Validation("invalid object key")
			}
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

	baseParsed, err := url.Parse(uc.cdnBaseURL)
	if err != nil || baseParsed.Host == "" {
		return nil, apperr.System("invalid CDN base URL configuration", err)
	}

	sanitizedFiles := make([]string, 0, len(in.Files))
	for _, f := range in.Files {
		u, parseErr := url.Parse(strings.TrimSpace(f))
		if parseErr != nil || u.Scheme != baseParsed.Scheme || !strings.EqualFold(u.Host, baseParsed.Host) || u.User != nil {
			return nil, apperr.Validation("all purge URLs must belong to the configured CDN domain")
		}
		if strings.Contains(u.RawPath, "%2e") || strings.Contains(u.RawPath, "%2E") || strings.Contains(u.Path, "..") || strings.Contains(u.Path, `\`) {
			return nil, apperr.Validation("purge URL contains disallowed path traversal")
		}
		cleanKey := strings.TrimPrefix(path.Clean("/"+u.Path), "/")
		if in.Actor.Role != "admin" {
			if _, ownErr := model.ValidateOwnedKey(cleanKey, in.Actor.UserID); ownErr != nil {
				return nil, apperr.Forbidden("forbidden to purge assets owned by another user")
			}
		} else if !strings.HasPrefix(cleanKey, "assets/") && !strings.HasPrefix(cleanKey, "private/") {
			return nil, apperr.Validation("purge URL must target an asset key")
		}
		sanitizedFiles = append(sanitizedFiles, fmt.Sprintf("%s/%s", uc.cdnBaseURL, cleanKey))
	}

	if err := uc.purger.PurgeURLs(ctx, sanitizedFiles); err != nil {
		return nil, apperr.External("failed to purge CDN cache", err)
	}

	return &PurgeCacheOutput{
		Status: "purged",
		Count:  len(sanitizedFiles),
	}, nil
}
