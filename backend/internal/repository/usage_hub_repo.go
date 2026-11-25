package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userUsageHubRepository struct {
	db *sql.DB
}

// NewUserUsageHubRepository 创建用户自助用量 Hub 仓储实现。
func NewUserUsageHubRepository(db *sql.DB) service.UserUsageHubRepository {
	return &userUsageHubRepository{db: db}
}

func (r *userUsageHubRepository) GetByUserID(ctx context.Context, userID int64) (*service.UserUsageHubRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("user usage hub repository db is nil")
	}

	const query = `
SELECT c.user_id, u.role, u.status, c.enabled, COALESCE(c.token_hash, ''), c.token_masked,
       c.include_sub2api, c.include_disabled, c.expose_reset_credits, c.magpie_sources,
       c.created_at, c.updated_at
FROM user_usage_hub_configs c
JOIN users u ON u.id = c.user_id
WHERE c.user_id = $1 AND u.deleted_at IS NULL`

	return scanUserUsageHubRecord(r.db.QueryRowContext(ctx, query, userID))
}

func (r *userUsageHubRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*service.UserUsageHubRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("user usage hub repository db is nil")
	}

	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, service.ErrUsageHubUserConfigNotFound
	}

	const query = `
SELECT c.user_id, u.role, u.status, c.enabled, COALESCE(c.token_hash, ''), c.token_masked,
       c.include_sub2api, c.include_disabled, c.expose_reset_credits, c.magpie_sources,
       c.created_at, c.updated_at
FROM user_usage_hub_configs c
JOIN users u ON u.id = c.user_id
WHERE c.token_hash = $1 AND u.deleted_at IS NULL AND u.status = 'active'`

	return scanUserUsageHubRecord(r.db.QueryRowContext(ctx, query, tokenHash))
}

func (r *userUsageHubRepository) Upsert(ctx context.Context, record *service.UserUsageHubRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("user usage hub repository db is nil")
	}
	if record == nil || record.UserID <= 0 {
		return fmt.Errorf("invalid user usage hub record")
	}

	sources := record.MagpieSources
	if sources == nil {
		sources = []service.UsageHubMagpieSourceRecord{}
	}
	sourcesJSON, err := json.Marshal(sources)
	if err != nil {
		return fmt.Errorf("marshal magpie sources: %w", err)
	}

	var tokenHash sql.NullString
	if trimmed := strings.TrimSpace(record.TokenHash); trimmed != "" {
		tokenHash = sql.NullString{String: trimmed, Valid: true}
	}

	now := time.Now().UTC()
	const stmt = `
INSERT INTO user_usage_hub_configs (
    user_id, enabled, token_hash, token_masked,
    include_sub2api, include_disabled, expose_reset_credits,
    magpie_sources, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $9)
ON CONFLICT (user_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    token_hash = EXCLUDED.token_hash,
    token_masked = EXCLUDED.token_masked,
    include_sub2api = EXCLUDED.include_sub2api,
    include_disabled = EXCLUDED.include_disabled,
    expose_reset_credits = EXCLUDED.expose_reset_credits,
    magpie_sources = EXCLUDED.magpie_sources,
    updated_at = EXCLUDED.updated_at`

	_, err = r.db.ExecContext(
		ctx,
		stmt,
		record.UserID,
		record.Enabled,
		tokenHash,
		record.TokenMasked,
		record.IncludeSub2API,
		record.IncludeDisabled,
		record.ExposeResetCredits,
		string(sourcesJSON),
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert user usage hub config: %w", err)
	}
	record.UpdatedAt = now
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	return nil
}

func (r *userUsageHubRepository) DeleteToken(ctx context.Context, userID int64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("user usage hub repository db is nil")
	}
	const stmt = `
UPDATE user_usage_hub_configs
SET enabled = FALSE, token_hash = NULL, token_masked = '', updated_at = $2
WHERE user_id = $1`
	_, err := r.db.ExecContext(ctx, stmt, userID, time.Now().UTC())
	return err
}

func (r *userUsageHubRepository) CountEnabled(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	const query = `
SELECT COUNT(*)
FROM user_usage_hub_configs c
JOIN users u ON u.id = c.user_id
WHERE c.enabled = TRUE
  AND c.token_hash IS NOT NULL
  AND c.token_hash <> ''
  AND u.deleted_at IS NULL
  AND u.status = 'active'`
	var count int64
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanUserUsageHubRecord(row *sql.Row) (*service.UserUsageHubRecord, error) {
	var (
		rec        service.UserUsageHubRecord
		rawSources []byte
	)
	err := row.Scan(
		&rec.UserID,
		&rec.UserRole,
		&rec.UserStatus,
		&rec.Enabled,
		&rec.TokenHash,
		&rec.TokenMasked,
		&rec.IncludeSub2API,
		&rec.IncludeDisabled,
		&rec.ExposeResetCredits,
		&rawSources,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUsageHubUserConfigNotFound
		}
		return nil, err
	}
	if len(rawSources) > 0 {
		if unmarshalErr := json.Unmarshal(rawSources, &rec.MagpieSources); unmarshalErr != nil {
			rec.MagpieSources = nil
		}
	}
	if rec.MagpieSources == nil {
		rec.MagpieSources = []service.UsageHubMagpieSourceRecord{}
	}
	return &rec, nil
}
