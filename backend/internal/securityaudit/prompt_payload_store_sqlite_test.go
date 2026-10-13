package securityaudit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func openPromptAuditSQLitePayloadStore(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:prompt_audit_payload?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE kv_store (
			key TEXT PRIMARY KEY,
			value BLOB NOT NULL,
			expires_at INTEGER,
			updated_at TEXT NOT NULL DEFAULT ''
		)`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestSQLitePayloadStoreRoundTripTTLAndDelete(t *testing.T) {
	store := NewSQLitePayloadStore(openPromptAuditSQLitePayloadStore(t))
	ctx := context.Background()
	const jobID int64 = 42
	const canary = "SQLITE_PROMPT_CANARY"

	require.NoError(t, store.Set(ctx, jobID, canary, 2*DefaultPayloadTTL))
	value, err := store.Get(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, canary, value)
	require.NoError(t, store.Ping(ctx))
	require.NoError(t, store.Delete(ctx, jobID))
	_, err = store.Get(ctx, jobID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestSQLitePayloadStoreExpires(t *testing.T) {
	db := openPromptAuditSQLitePayloadStore(t)
	store := NewSQLitePayloadStore(db)
	ctx := context.Background()
	const jobID int64 = 43

	require.NoError(t, store.Set(ctx, jobID, "expired", time.Minute))
	_, err := db.ExecContext(ctx, `UPDATE kv_store SET expires_at = ? WHERE key = ?`, time.Now().Add(-time.Second).UnixMilli(), payloadKey(jobID))
	require.NoError(t, err)
	_, err = store.Get(ctx, jobID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
