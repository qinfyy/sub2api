package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestOpsSQLiteAdvisoryLockUsesLocalMutex(t *testing.T) {
	db, err := sql.Open("sqlite", "file:ops_advisory_lock?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.True(t, isSQLiteDB(db))

	release, acquired := tryAcquireDBAdvisoryLock(context.Background(), db, 4242)
	require.True(t, acquired)
	require.NotNil(t, release)

	_, second := tryAcquireDBAdvisoryLock(context.Background(), db, 4242)
	require.False(t, second, "the same SQLite process lock must be single-flight")

	release()
	releaseAgain, acquiredAgain := tryAcquireDBAdvisoryLock(context.Background(), db, 4242)
	require.True(t, acquiredAgain)
	releaseAgain()
}
