package service

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
)

func hashAdvisoryLockID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}

func tryAcquireDBAdvisoryLock(ctx context.Context, db *sql.DB, lockID int64) (func(), bool) {
	release, acquired, _ := tryAcquireDBAdvisoryLockWithError(ctx, db, lockID)
	return release, acquired
}

// opsSQLiteAdvisoryLocks keeps the in-process fallback used when a SQLite-backed
// deployment reaches the DB advisory-lock path. SQLite itself has no advisory
// locks; the file guard already enforces single-instance deployment, so a
// process-local mutex is sufficient to keep concurrent local workers single-flight.
var opsSQLiteAdvisoryLocks sync.Map

func tryAcquireSQLiteAdvisoryLock(lockID int64) (func(), bool) {
	value, _ := opsSQLiteAdvisoryLocks.LoadOrStore(lockID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

func isSQLiteDB(db *sql.DB) bool {
	if db == nil {
		return false
	}
	return strings.Contains(strings.ToLower(fmt.Sprintf("%T", db.Driver())), "sqlite")
}

func tryAcquireDBAdvisoryLockWithError(ctx context.Context, db *sql.DB, lockID int64) (func(), bool, error) {
	if db == nil {
		return nil, false, nil
	}
	if isSQLiteDB(db) {
		release, acquired := tryAcquireSQLiteAdvisoryLock(lockID)
		return release, acquired, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("open advisory-lock connection: %w", err)
	}

	acquired := false
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("query advisory lock: %w", err)
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}

	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", lockID)
		_ = conn.Close()
	}
	return release, true, nil
}
