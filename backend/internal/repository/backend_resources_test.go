package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newMemoryStateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := &config.Config{Database: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, Path: filepath.Join(t.TempDir(), "state.db")}}
	drv, err := openSQLiteDriver(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	require.NoError(t, initSQLiteKVSchema(context.Background(), drv.DB()))
	return drv.DB()
}

func TestBackendResourcesSQLiteMemoryLifecycle(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "main.db")
	cfg.Redis.Type = config.CacheTypeMemory
	cfg.JWT.Secret = strings.Repeat("a", 32)
	r, closeResources, err := ProvideBackendResources(cfg)
	require.NoError(t, err)
	t.Cleanup(closeResources)
	require.Nil(t, r.Redis)
	require.Same(t, r.SQL, r.State.DB)
	require.False(t, r.State.owned)
	require.NotNil(t, r.memory)
	require.EqualValues(t, 512*1024*1024, r.memory.maxBytes)
	require.NoError(t, r.SQL.Ping())
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
	require.Error(t, r.SQL.Ping())
	guard, err := acquireSingleInstanceLock(cfg.Database.Path + ".lock")
	require.NoError(t, err)
	require.NoError(t, ReleaseSingleInstanceGuard(guard))
}

func TestBackendResourcesFailureReleasesSQLiteGuard(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "failed.db")
	cfg.Redis.Type = config.CacheTypeMemory
	cfg.Redis.Memory.MaxCapacityMB = -1
	r, cleanup, err := ProvideBackendResources(cfg)
	require.Error(t, err)
	require.Nil(t, r)
	require.Nil(t, cleanup)
	guard, err := acquireSingleInstanceLock(cfg.Database.Path + ".lock")
	require.NoError(t, err)
	require.NoError(t, ReleaseSingleInstanceGuard(guard))
}

func TestDurableStateUsesSeparateSQLiteForPostgres(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Redis.Type = config.CacheTypeMemory
	cfg.Redis.Memory.PersistencePath = filepath.Join(t.TempDir(), "cache.db")
	state, guard, err := openDurableStateDB(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = state.DB.Close(); _ = ReleaseSingleInstanceGuard(guard) }()
	require.True(t, state.owned)
	require.True(t, isSQLiteDB(state.DB))
	_, err = state.DB.Exec(`INSERT INTO kv_store(key,value) VALUES('test',X'00')`)
	require.NoError(t, err)
	_, _, err = openDurableStateDB(cfg, nil)
	require.ErrorIs(t, err, ErrSingleInstanceViolation)
}

func TestSQLiteRedisAlsoRequiresFileGuard(t *testing.T) {
	cfg := sqliteMemoryConfig(t)
	cfg.Redis.Type = config.CacheTypeRedis
	guard, err := ValidateSingleInstanceGuard(cfg)
	require.NoError(t, err)
	require.NotNil(t, guard)
	defer func() { _ = ReleaseSingleInstanceGuard(guard) }()
	_, err = acquireSingleInstanceLock(cfg.Database.Path + ".lock")
	require.ErrorIs(t, err, ErrSingleInstanceViolation)
}

func TestPostgresDeploymentLockModes(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "exclusive"}[exclusive], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			query := `SELECT pg_try_advisory_lock_shared\(\$1, \$2\)`
			if exclusive {
				query = `SELECT pg_try_advisory_lock\(\$1, \$2\)`
			}
			mock.ExpectQuery(query).WithArgs(deploymentLockNamespace, deploymentLockID).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
			guard, err := acquirePostgresInstanceGuard(context.Background(), db, exclusive)
			require.NoError(t, err)
			mock.ExpectClose()
			require.NoError(t, guard.Close())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresDeploymentLockConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(false))
	g, err := acquirePostgresInstanceGuard(context.Background(), db, true)
	require.Nil(t, g)
	require.ErrorIs(t, err, ErrSingleInstanceViolation)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBackendResourcesReportsLostDeploymentSession(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	g, err := acquirePostgresInstanceGuard(context.Background(), db, true)
	require.NoError(t, err)
	mock.ExpectPing().WillReturnError(errors.New("session lost"))
	ctx, cancel := context.WithCancel(context.Background())
	r := &BackendResources{pgGuard: g, cancel: cancel, done: make(chan struct{}), lost: make(chan error, 1)}
	go r.maintain(ctx, 60)
	select {
	case err := <-r.Lost():
		require.ErrorContains(t, err, "session lost")
	case <-time.After(5 * time.Second):
		t.Fatal("lock loss not reported")
	}
	mock.ExpectClose()
	require.NoError(t, r.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}
