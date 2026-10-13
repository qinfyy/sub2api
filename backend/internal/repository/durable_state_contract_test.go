package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSQLitePrimaryDurableStateContract(t *testing.T) {
	testPrimaryDurableStateContract(t, newMemoryStateTestDB(t))
}

func testPrimaryDurableStateContract(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	prefix := "state-contract:" + uuid.NewString() + ":"
	first := newMemoryKVBackend(newMemoryCacheCore(1), db)
	second := newMemoryKVBackend(newMemoryCacheCore(1), db)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := db.ExecContext(cleanup, `DELETE FROM kv_store WHERE key LIKE $1`, prefix+"%")
		require.NoError(t, err)
	})
	payload := []byte{0, 255, 1, '"', '\\'}
	require.NoError(t, first.setBytes(ctx, prefix+"value", payload, time.Hour))
	loaded, err := second.get(ctx, prefix+"value")
	require.NoError(t, err)
	require.Equal(t, payload, loaded)
	require.NoError(t, first.setBytes(ctx, prefix+"value", payload, 0))
	var expiry sql.NullInt64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT expires_at FROM kv_store WHERE key=$1`, prefix+"value").Scan(&expiry))
	require.False(t, expiry.Valid, "upsert must clear an earlier expiry")
	for _, key := range []string{"literal_a", "literalXa", "LITERAL_a"} {
		require.NoError(t, first.setBytes(ctx, prefix+key, []byte{}, 0))
	}
	keys, err := second.state.list(ctx, prefix+"literal_", 10)
	require.NoError(t, err)
	require.Equal(t, []string{prefix + "literal_a"}, keys)
	require.NoError(t, first.addMember(ctx, prefix+"ttl-members", "first", time.Hour))
	var initialExpiry, updatedExpiry int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT expires_at FROM kv_store WHERE key=$1`, prefix+"ttl-members").Scan(&initialExpiry))
	require.NoError(t, second.addMember(ctx, prefix+"ttl-members", "second", time.Minute))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT expires_at FROM kv_store WHERE key=$1`, prefix+"ttl-members").Scan(&updatedExpiry))
	require.Equal(t, initialExpiry, updatedExpiry, "shorter member TTL must not shift the existing absolute expiry")
	abort := errors.New("abort outer transaction")
	err = NewTransactionRunner(first.state.client).WithinTransaction(ctx, func(ctx context.Context) error {
		claimed, err := second.setNX(ctx, prefix+"once", payload, time.Hour)
		require.NoError(t, err)
		require.True(t, claimed)
		claimed, err = first.setNX(ctx, prefix+"once", payload, time.Hour)
		require.NoError(t, err)
		require.False(t, claimed)
		value, err := second.consume(ctx, prefix+"once")
		require.NoError(t, err)
		require.Equal(t, payload, value)
		_, err = first.consume(ctx, prefix+"once")
		require.ErrorIs(t, err, errMemoryCacheMiss)
		require.NoError(t, first.addMember(ctx, prefix+"members", "first", time.Hour))
		require.NoError(t, second.addMember(ctx, prefix+"members", "second", time.Minute))
		members, err := first.members(ctx, prefix+"members")
		require.NoError(t, err)
		require.Equal(t, []string{"first", "second"}, members)
		require.NoError(t, second.state.deleteSetMembers(ctx, prefix+"members", prefix))
		require.NoError(t, second.setBytes(ctx, prefix+"rolled-back", payload, 0))
		return abort
	})
	require.ErrorIs(t, err, abort)
	_, err = first.get(ctx, prefix+"rolled-back")
	require.ErrorIs(t, err, errMemoryCacheMiss)
	_, err = first.get(ctx, prefix+"members")
	require.ErrorIs(t, err, errMemoryCacheMiss)
}
