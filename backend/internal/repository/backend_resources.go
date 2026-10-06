package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/redis/go-redis/v9"
)

// DurableStateDB is deliberately distinct from the application's primary DB.
// In memory mode it always contains SQLite KV tables, even with a PG primary.
type DurableStateDB struct {
	DB    *sql.DB
	owned bool
}

// BackendResources owns infrastructure only. Workers must stop before Close.
type BackendResources struct {
	Ent        *ent.Client
	SQL        *sql.DB
	Redis      *redis.Client
	State      *DurableStateDB
	memory     *memoryCacheCore
	fileGuard  *singleInstanceGuard
	stateGuard *singleInstanceGuard
	pgGuard    *postgresInstanceGuard
	cancel     context.CancelFunc
	done       chan struct{}
	lost       chan error
	closeOnce  sync.Once
	closeErr   error
}

// Lost reports loss of the deployment lock. The application must stop serving.
func (r *BackendResources) Lost() <-chan error { return r.lost }

// ProvideBackendResources returns a Wire cleanup, also used on injection errors.
func ProvideBackendResources(cfg *config.Config) (_ *BackendResources, cleanup func(), err error) {
	if cfg == nil {
		return nil, nil, errors.New("nil backend config")
	}
	cfg.Database.NormalizeType()
	cfg.Redis.NormalizeType()
	if cfg.Database.Type != config.DatabaseTypeSQLite && cfg.Database.Type != config.DatabaseTypePostgres {
		return nil, nil, fmt.Errorf("unsupported database type %q", cfg.Database.Type)
	}
	if cfg.Redis.Type != config.CacheTypeMemory && cfg.Redis.Type != config.CacheTypeRedis {
		return nil, nil, fmt.Errorf("unsupported cache type %q", cfg.Redis.Type)
	}
	r := &BackendResources{lost: make(chan error, 1)}
	defer func() {
		if err != nil {
			_ = r.Close()
		}
	}()
	if cfg.Database.IsSQLite() {
		if err = cfg.Database.ResolveSQLitePath("."); err != nil {
			return nil, nil, err
		}
		r.fileGuard, err = ValidateSingleInstanceGuard(cfg)
	} else {
		r.pgGuard, err = openPostgresInstanceGuard(cfg)
	}
	if err != nil {
		return nil, nil, err
	}
	r.Ent, r.SQL, err = InitEnt(cfg)
	if err != nil {
		return nil, nil, err
	}
	if cfg.Redis.IsMemory() {
		if cfg.Redis.Memory.MaxCapacityMB <= 0 || cfg.Redis.Memory.GCIntervalSeconds <= 0 {
			return nil, nil, errors.New("memory capacity and GC interval must be positive")
		}
		r.State, r.stateGuard, err = openDurableStateDB(cfg, r.SQL)
		if err != nil {
			return nil, nil, err
		}
		r.memory = newMemoryCacheCore(0)
		r.memory.maxBytes = int64(cfg.Redis.Memory.MaxCapacityMB) * 1024 * 1024
	} else {
		r.Redis = InitRedis(cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	go r.maintain(ctx, cfg.Redis.Memory.GCIntervalSeconds)
	return r, func() {
		if err := r.Close(); err != nil {
			slog.Error("close backend resources", "error", err)
		}
	}, nil
}

func openDurableStateDB(cfg *config.Config, primary *sql.DB) (*DurableStateDB, *singleInstanceGuard, error) {
	if cfg.Database.IsSQLite() {
		if !isSQLiteDB(primary) {
			return nil, nil, errors.New("memory state requires a SQLite connection")
		}
		return &DurableStateDB{DB: primary}, nil, nil
	}
	if err := cfg.Redis.Memory.ResolvePersistencePath("."); err != nil {
		return nil, nil, err
	}
	stateCfg := &config.Config{Database: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, Path: cfg.Redis.Memory.PersistencePath}}
	lockPath, err := singleInstanceLockPath(stateCfg)
	if err != nil {
		return nil, nil, err
	}
	guard, err := acquireSingleInstanceLock(lockPath)
	if err != nil {
		return nil, nil, err
	}
	drv, err := openSQLiteDriver(stateCfg)
	if err != nil {
		_ = ReleaseSingleInstanceGuard(guard)
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := initSQLiteKVSchema(ctx, drv.DB()); err != nil {
		_ = drv.Close()
		_ = ReleaseSingleInstanceGuard(guard)
		return nil, nil, err
	}
	return &DurableStateDB{DB: drv.DB(), owned: true}, guard, nil
}

func (r *BackendResources) maintain(ctx context.Context, gcSeconds int) {
	defer close(r.done)
	if gcSeconds <= 0 {
		gcSeconds = 60
	}
	gc := time.NewTicker(time.Duration(gcSeconds) * time.Second)
	defer gc.Stop()
	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if r.pgGuard != nil {
				probe, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := r.pgGuard.conn.PingContext(probe)
				cancel()
				if err != nil && ctx.Err() == nil {
					r.lost <- fmt.Errorf("deployment lock connection lost: %w", err)
					return
				}
			}
		case <-gc.C:
			if r.memory != nil {
				r.memory.gc()
			}
			if r.State != nil {
				work, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := r.State.DB.ExecContext(work, `DELETE FROM kv_store WHERE key IN (SELECT key FROM kv_store WHERE expires_at IS NOT NULL AND expires_at <= ? LIMIT 1000)`, time.Now().UnixMilli())
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Warn("expire durable cache state", "error", err)
				}
			}
		}
	}
}

func (r *BackendResources) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
			<-r.done
		}
		var errs []error
		if r.memory != nil {
			r.memory.events.close()
		}
		if r.Redis != nil {
			errs = append(errs, r.Redis.Close())
		}
		if r.State != nil && r.State.owned {
			errs = append(errs, r.State.DB.Close())
		}
		if r.Ent != nil {
			errs = append(errs, r.Ent.Close())
		}
		if r.pgGuard != nil {
			errs = append(errs, r.pgGuard.Close())
		}
		errs = append(errs, ReleaseSingleInstanceGuard(r.stateGuard), ReleaseSingleInstanceGuard(r.fileGuard))
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}
