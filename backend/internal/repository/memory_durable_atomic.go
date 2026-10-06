package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (b *memoryKVBackend) setNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if b.db == nil {
		return memorySetNX(b.core, key, append([]byte(nil), value...), ttl), nil
	}
	var expires any
	if ttl > 0 {
		expires = time.Now().Add(ttl).UnixMilli()
	}
	var returned string
	err := b.db.QueryRowContext(ctx, `INSERT INTO kv_store(key,value,expires_at,updated_at) VALUES(?,?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value,expires_at=excluded.expires_at,updated_at=excluded.updated_at
WHERE kv_store.expires_at IS NOT NULL AND kv_store.expires_at<=?
RETURNING key`, key, value, expires, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UnixMilli()).Scan(&returned)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
