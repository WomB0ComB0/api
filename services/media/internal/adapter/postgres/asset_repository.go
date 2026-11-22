package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"api/media/internal/domain/model"
	"api/media/internal/domain/port"
	"api/media/internal/pkg/apperr"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ port.AssetRepository = (*AssetRepository)(nil)

type AssetRepository struct {
	pool *pgxpool.Pool
}

func NewAssetRepository(ctx context.Context, databaseURL string) (*AssetRepository, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 15
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &AssetRepository{pool: pool}, nil
}

func (r *AssetRepository) Save(ctx context.Context, asset *model.Asset) (*model.Asset, error) {
	metaBytes, err := json.Marshal(asset.Metadata)
	if err != nil || len(metaBytes) == 0 || string(metaBytes) == "null" {
		metaBytes = []byte("{}")
	}

	const query = `
		INSERT INTO media.assets (user_id, filename, mime_type, size_bytes, r2_key, public_url, visibility, etag, sha256, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (r2_key) DO UPDATE SET
			filename = EXCLUDED.filename,
			mime_type = EXCLUDED.mime_type,
			size_bytes = EXCLUDED.size_bytes,
			etag = EXCLUDED.etag,
			sha256 = EXCLUDED.sha256,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
		RETURNING id::text, user_id::text, filename, mime_type, size_bytes, r2_key, public_url, visibility, COALESCE(etag, ''), COALESCE(sha256, ''), metadata, created_at, updated_at
	`

	return r.scanOne(ctx, query,
		asset.UserID,
		asset.Filename,
		asset.MimeType,
		asset.SizeBytes,
		asset.R2Key,
		asset.PublicURL,
		string(asset.Visibility),
		asset.ETag,
		asset.SHA256,
		metaBytes,
	)
}

func (r *AssetRepository) FindByID(ctx context.Context, id string) (*model.Asset, error) {
	const query = `
		SELECT id::text, user_id::text, filename, mime_type, size_bytes, r2_key, public_url, visibility, COALESCE(etag, ''), COALESCE(sha256, ''), metadata, created_at, updated_at
		FROM media.assets
		WHERE id = $1
	`
	return r.scanOne(ctx, query, id)
}

func (r *AssetRepository) FindByKey(ctx context.Context, key string) (*model.Asset, error) {
	const query = `
		SELECT id::text, user_id::text, filename, mime_type, size_bytes, r2_key, public_url, visibility, COALESCE(etag, ''), COALESCE(sha256, ''), metadata, created_at, updated_at
		FROM media.assets
		WHERE r2_key = $1
	`
	return r.scanOne(ctx, query, key)
}

func (r *AssetRepository) scanOne(ctx context.Context, query string, args ...any) (*model.Asset, error) {
	var a model.Asset
	var vis string
	var rawMeta []byte
	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&a.ID,
		&a.UserID,
		&a.Filename,
		&a.MimeType,
		&a.SizeBytes,
		&a.R2Key,
		&a.PublicURL,
		&vis,
		&a.ETag,
		&a.SHA256,
		&rawMeta,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("asset not found")
	}
	if err != nil {
		return nil, apperr.System("database query failed", err)
	}
	a.Visibility = model.Visibility(vis)
	if len(rawMeta) > 0 {
		_ = json.Unmarshal(rawMeta, &a.Metadata)
	}
	return &a, nil
}

func (r *AssetRepository) FindByIDs(ctx context.Context, ids []string) ([]model.Asset, error) {
	if len(ids) == 0 {
		return []model.Asset{}, nil
	}
	const query = `
		SELECT id::text, user_id::text, filename, mime_type, size_bytes, r2_key, public_url, visibility, COALESCE(etag, ''), COALESCE(sha256, ''), metadata, created_at, updated_at
		FROM media.assets
		WHERE id = ANY($1::uuid[])
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, apperr.System("failed to batch fetch assets", err)
	}
	defer rows.Close()

	assets := make([]model.Asset, 0, len(ids))
	for rows.Next() {
		var a model.Asset
		var vis string
		var rawMeta []byte
		if err := rows.Scan(
			&a.ID,
			&a.UserID,
			&a.Filename,
			&a.MimeType,
			&a.SizeBytes,
			&a.R2Key,
			&a.PublicURL,
			&vis,
			&a.ETag,
			&a.SHA256,
			&rawMeta,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, apperr.System("failed to scan asset row", err)
		}
		a.Visibility = model.Visibility(vis)
		if len(rawMeta) > 0 {
			_ = json.Unmarshal(rawMeta, &a.Metadata)
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (r *AssetRepository) ListByUser(ctx context.Context, userID string, limit, offset int) ([]model.Asset, int, error) {
	// Single query using COUNT(*) OVER() — avoids separate COUNT roundtrip and prevents N+1
	const query = `
		SELECT id::text, user_id::text, filename, mime_type, size_bytes, r2_key, public_url, visibility, COALESCE(etag, ''), COALESCE(sha256, ''), metadata, created_at, updated_at, COUNT(*) OVER() AS total_count
		FROM media.assets
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, 0, apperr.System("failed to list assets", err)
	}
	defer rows.Close()

	assets := make([]model.Asset, 0)
	total := 0
	for rows.Next() {
		var a model.Asset
		var vis string
		var rawMeta []byte
		var totalCount int
		if err := rows.Scan(
			&a.ID,
			&a.UserID,
			&a.Filename,
			&a.MimeType,
			&a.SizeBytes,
			&a.R2Key,
			&a.PublicURL,
			&vis,
			&a.ETag,
			&a.SHA256,
			&rawMeta,
			&a.CreatedAt,
			&a.UpdatedAt,
			&totalCount,
		); err != nil {
			return nil, 0, apperr.System("failed to scan asset row", err)
		}
		total = totalCount
		a.Visibility = model.Visibility(vis)
		if len(rawMeta) > 0 {
			_ = json.Unmarshal(rawMeta, &a.Metadata)
		}
		assets = append(assets, a)
	}
	return assets, total, rows.Err()
}

func (r *AssetRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM media.assets WHERE id = $1", id)
	if err != nil {
		return apperr.System("failed to delete asset", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("asset not found")
	}
	return nil
}

func (r *AssetRepository) HealthCheck(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *AssetRepository) Close() {
	r.pool.Close()
}
