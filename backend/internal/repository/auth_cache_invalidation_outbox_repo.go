package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type authCacheInvalidationOutboxRepository struct {
	db     *sql.DB
	sqlite bool
}

func NewAuthCacheInvalidationOutboxRepository(db *sql.DB) service.AuthCacheInvalidationOutboxRepository {
	return &authCacheInvalidationOutboxRepository{db: db, sqlite: isSQLiteDB(db)}
}

func (r *authCacheInvalidationOutboxRepository) Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.AuthCacheInvalidationEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil auth cache invalidation outbox database")
	}
	if limit <= 0 {
		limit = 100
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 30
	}
	if r.sqlite {
		return r.claimSQLite(ctx, workerID, limit, leaseSeconds)
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id
			FROM auth_cache_invalidation_outbox
			WHERE available_at <= NOW()
			  AND (claimed_at IS NULL OR claimed_at < NOW() - ($3 * INTERVAL '1 second'))
			ORDER BY id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE auth_cache_invalidation_outbox AS o
		SET claimed_at = NOW(), claimed_by = $1
		FROM candidates AS c
		WHERE o.id = c.id
		RETURNING o.id, o.cache_key, o.attempts, o.delivery_stage, o.created_at
	`, workerID, limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	events := make([]service.AuthCacheInvalidationEvent, 0, limit)
	for rows.Next() {
		var event service.AuthCacheInvalidationEvent
		if err := rows.Scan(&event.ID, &event.CacheKey, &event.Attempts, &event.Stage, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.CacheKey = strings.TrimSpace(event.CacheKey)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// claimSQLite 是 Claim 的 SQLite 方言实现。
//
// SQLite 没有 FOR UPDATE SKIP LOCKED；连接池上限为 1 且写事务串行，因此用
// 单事务完成"选行 + 标记领取"，避免两个 worker 拿到同一批事件。
func (r *authCacheInvalidationOutboxRepository) claimSQLite(ctx context.Context, workerID string, limit int, leaseSeconds int64) ([]service.AuthCacheInvalidationEvent, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(leaseSeconds) * time.Second)
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM auth_cache_invalidation_outbox
		WHERE julianday(available_at) <= julianday(?)
		  AND (claimed_at IS NULL OR julianday(claimed_at) < julianday(?))
		ORDER BY id ASC
		LIMIT ?
	`, nowSQLiteUTC(), cutoff.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []service.AuthCacheInvalidationEvent{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+2)
	args = append(args, nowSQLiteUTC(), workerID)
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE auth_cache_invalidation_outbox SET claimed_at = ?, claimed_by = ? WHERE id IN (`+placeholders+`)`,
		args...,
	); err != nil {
		return nil, err
	}

	events := make([]service.AuthCacheInvalidationEvent, 0, len(ids))
	for _, id := range ids {
		var (
			event      service.AuthCacheInvalidationEvent
			createdRaw string
		)
		if err := tx.QueryRowContext(ctx, `
			SELECT id, cache_key, attempts, delivery_stage, created_at
			FROM auth_cache_invalidation_outbox
			WHERE id = ?
		`, id).Scan(&event.ID, &event.CacheKey, &event.Attempts, &event.Stage, &createdRaw); err != nil {
			return nil, err
		}
		createdAt, err := parseSQLiteTime(createdRaw)
		if err != nil {
			return nil, err
		}
		event.CreatedAt = createdAt
		event.CacheKey = strings.TrimSpace(event.CacheKey)
		events = append(events, event)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *authCacheInvalidationOutboxRepository) ScheduleSecondPass(ctx context.Context, id int64, workerID string, availableAt time.Time) error {
	if r.sqlite {
		return r.updateClaimedSQLite(ctx,
			`UPDATE auth_cache_invalidation_outbox
			SET delivery_stage = 1,
				available_at = ?,
				last_error = NULL,
				claimed_at = NULL,
				claimed_by = NULL
			WHERE id = ? AND claimed_by = ? AND delivery_stage = 0`,
			id, workerID, fmt.Errorf("auth cache invalidation claim %d cannot schedule second pass", id),
			availableAt.UTC().Format(time.RFC3339Nano))
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE auth_cache_invalidation_outbox
		SET delivery_stage = 1,
			available_at = $3,
			last_error = NULL,
			claimed_at = NULL,
			claimed_by = NULL
		WHERE id = $1 AND claimed_by = $2 AND delivery_stage = 0
	`, id, workerID, availableAt)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("auth cache invalidation claim %d cannot schedule second pass", id)
	}
	return nil
}

func (r *authCacheInvalidationOutboxRepository) DeleteClaimed(ctx context.Context, id int64, workerID string) error {
	query := `
		DELETE FROM auth_cache_invalidation_outbox
		WHERE id = $1 AND claimed_by = $2
	`
	if r.sqlite {
		query = `
			DELETE FROM auth_cache_invalidation_outbox
			WHERE id = ? AND claimed_by = ?
		`
	}
	result, err := r.db.ExecContext(ctx, query, id, workerID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("auth cache invalidation claim %d is no longer owned by %s", id, workerID)
	}
	return nil
}

func (r *authCacheInvalidationOutboxRepository) RetryClaimed(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error {
	if r.sqlite {
		return r.updateClaimedSQLite(ctx,
			`UPDATE auth_cache_invalidation_outbox
			SET attempts = attempts + 1,
				available_at = ?,
				last_error = ?,
				claimed_at = NULL,
				claimed_by = NULL
			WHERE id = ? AND claimed_by = ?`,
			id, workerID, fmt.Errorf("auth cache invalidation claim %d is no longer owned by %s", id, workerID),
			availableAt.UTC().Format(time.RFC3339Nano), lastError)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE auth_cache_invalidation_outbox
		SET attempts = attempts + 1,
			available_at = $3,
			last_error = $4,
			claimed_at = NULL,
			claimed_by = NULL
		WHERE id = $1 AND claimed_by = $2
	`, id, workerID, availableAt, lastError)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("auth cache invalidation claim %d is no longer owned by %s", id, workerID)
	}
	return nil
}

// updateClaimedSQLite 执行 SQLite 方言的领取状态更新，并校验受影响行数。
// params 依次是 SET 子句参数，最后两个是 id 与 workerID。
func (r *authCacheInvalidationOutboxRepository) updateClaimedSQLite(
	ctx context.Context,
	query string,
	id int64,
	workerID string,
	mismatchErr error,
	setArgs ...any,
) error {
	args := append(append([]any{}, setArgs...), id, workerID)
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return mismatchErr
	}
	return nil
}

func (r *authCacheInvalidationOutboxRepository) Stats(ctx context.Context) (service.AuthCacheInvalidationOutboxStats, error) {
	if r.sqlite {
		return r.statsSQLite(ctx)
	}
	var (
		stats     service.AuthCacheInvalidationOutboxStats
		oldest    sql.NullTime
		lastError sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(created_at), COALESCE(MAX(attempts), 0),
			(SELECT last_error
			 FROM auth_cache_invalidation_outbox
			 WHERE last_error IS NOT NULL
			 ORDER BY available_at DESC, id DESC
			 LIMIT 1)
		FROM auth_cache_invalidation_outbox
	`).Scan(&stats.Pending, &oldest, &stats.MaxAttempts, &lastError)
	if err != nil {
		return stats, err
	}
	if oldest.Valid {
		value := oldest.Time
		stats.OldestCreatedAt = &value
	}
	if lastError.Valid {
		stats.LastError = lastError.String
	}
	return stats, nil
}

// statsSQLite 读取 SQLite 方言的 outbox 统计。SQLite 的 MIN(created_at)
// 返回文本，需要显式解析，不能直接扫描进 time.Time。
func (r *authCacheInvalidationOutboxRepository) statsSQLite(ctx context.Context) (service.AuthCacheInvalidationOutboxStats, error) {
	var (
		stats     service.AuthCacheInvalidationOutboxStats
		oldestRaw sql.NullString
		lastError sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(created_at), COALESCE(MAX(attempts), 0),
			(SELECT last_error
			 FROM auth_cache_invalidation_outbox
			 WHERE last_error IS NOT NULL
			 ORDER BY available_at DESC, id DESC
			 LIMIT 1)
		FROM auth_cache_invalidation_outbox
	`).Scan(&stats.Pending, &oldestRaw, &stats.MaxAttempts, &lastError)
	if err != nil {
		return stats, err
	}
	if oldestRaw.Valid {
		oldest, err := parseSQLiteTime(oldestRaw.String)
		if err != nil {
			return stats, err
		}
		if !oldest.IsZero() {
			stats.OldestCreatedAt = &oldest
		}
	}
	if lastError.Valid {
		stats.LastError = lastError.String
	}
	return stats, nil
}
