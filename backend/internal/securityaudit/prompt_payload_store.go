package securityaudit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type PayloadStore interface {
	Set(ctx context.Context, jobID int64, scanText string, ttl time.Duration) error
	Get(ctx context.Context, jobID int64) (string, error)
	Delete(ctx context.Context, jobID int64) error
	Ping(ctx context.Context) error
}

type RedisPayloadStore struct {
	client *redis.Client
}

// SQLitePayloadStore persists prompt payloads in the durable SQLite state
// database so audit jobs survive a restart without Redis.
type SQLitePayloadStore struct {
	db *sql.DB
}

// NewSQLitePayloadStore returns a payload store backed by the given SQLite
// connection. The caller owns the connection and is responsible for closing it.
func NewSQLitePayloadStore(db *sql.DB) *SQLitePayloadStore {
	return &SQLitePayloadStore{db: db}
}

func NewRedisPayloadStore(client *redis.Client) *RedisPayloadStore {
	return &RedisPayloadStore{client: client}
}

func (s *RedisPayloadStore) Set(ctx context.Context, jobID int64, scanText string, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("prompt audit payload store unavailable")
	}
	if jobID <= 0 || scanText == "" {
		return fmt.Errorf("prompt audit payload input invalid")
	}
	if ttl <= 0 || ttl > DefaultPayloadTTL {
		ttl = DefaultPayloadTTL
	}
	return s.client.Set(ctx, payloadKey(jobID), scanText, ttl).Err()
}

func (s *RedisPayloadStore) Get(ctx context.Context, jobID int64) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("prompt audit payload store unavailable")
	}
	return s.client.Get(ctx, payloadKey(jobID)).Result()
}

func (s *RedisPayloadStore) Delete(ctx context.Context, jobID int64) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("prompt audit payload store unavailable")
	}
	return s.client.Del(ctx, payloadKey(jobID)).Err()
}

func (s *RedisPayloadStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("prompt audit payload store unavailable")
	}
	return s.client.Ping(ctx).Err()
}

func (s *SQLitePayloadStore) Set(ctx context.Context, jobID int64, scanText string, ttl time.Duration) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("prompt audit payload store unavailable")
	}
	if jobID <= 0 || scanText == "" {
		return fmt.Errorf("prompt audit payload input invalid")
	}
	if ttl <= 0 || ttl > DefaultPayloadTTL {
		ttl = DefaultPayloadTTL
	}
	expiresAt := time.Now().Add(ttl).UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kv_store (key, value, expires_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(key) DO UPDATE SET
		  value = excluded.value,
		  expires_at = excluded.expires_at,
		  updated_at = excluded.updated_at`,
		payloadKey(jobID), []byte(scanText), expiresAt, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLitePayloadStore) Get(ctx context.Context, jobID int64) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("prompt audit payload store unavailable")
	}
	var (
		value     []byte
		expiresAt sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT value, expires_at FROM kv_store WHERE key = $1`, payloadKey(jobID),
	).Scan(&value, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	if expiresAt.Valid && time.Now().UnixMilli() >= expiresAt.Int64 {
		return "", sql.ErrNoRows
	}
	return string(value), nil
}

func (s *SQLitePayloadStore) Delete(ctx context.Context, jobID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("prompt audit payload store unavailable")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM kv_store WHERE key = $1`, payloadKey(jobID))
	return err
}

func (s *SQLitePayloadStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("prompt audit payload store unavailable")
	}
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

func payloadKey(jobID int64) string {
	return PayloadKeyPrefix + strconv.FormatInt(jobID, 10)
}
