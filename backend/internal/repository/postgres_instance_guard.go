package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// The two-key advisory lock is database-scoped and separate from migration locks.
const deploymentLockNamespace = 0x53324150
const deploymentLockID = 1

type postgresInstanceGuard struct {
	db   *sql.DB
	conn *sql.Conn
}

func openPostgresInstanceGuard(cfg *config.Config) (*postgresInstanceGuard, error) {
	db, err := sql.Open("postgres", cfg.Database.DSNWithTimezone(cfg.Timezone))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	guard, err := acquirePostgresInstanceGuard(ctx, db, cfg.Redis.IsMemory())
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return guard, nil
}

func acquirePostgresInstanceGuard(ctx context.Context, db *sql.DB, exclusive bool) (*postgresInstanceGuard, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	query := "SELECT pg_try_advisory_lock_shared($1, $2)"
	if exclusive {
		query = "SELECT pg_try_advisory_lock($1, $2)"
	}
	var acquired bool
	if err := conn.QueryRowContext(ctx, query, deploymentLockNamespace, deploymentLockID).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire deployment lock: %w", err)
	}
	if !acquired {
		_ = conn.Close()
		return nil, ErrSingleInstanceViolation
	}
	return &postgresInstanceGuard{db: db, conn: conn}, nil
}

func (g *postgresInstanceGuard) Close() error {
	// This pool is private and has no idle connections. Closing the session
	// releases its lock; it must never be returned to the application's pool.
	return errors.Join(g.conn.Close(), g.db.Close())
}
