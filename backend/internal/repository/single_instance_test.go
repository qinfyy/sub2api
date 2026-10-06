package repository

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// sqliteMemoryConfig 返回一个指向临时目录的 SQLite + 内存配置。
func sqliteMemoryConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := &config.Config{}
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "sub2api.db")
	cfg.Redis.Type = config.CacheTypeMemory
	return cfg
}

func TestValidateSingleInstanceGuardSkippedForOtherCombinations(t *testing.T) {
	tests := []struct {
		name    string
		dbType  string
		redis   string
		wantNil bool
	}{
		{name: "postgres + redis", dbType: config.DatabaseTypePostgres, redis: config.CacheTypeRedis, wantNil: true},
		{name: "postgres + memory", dbType: config.DatabaseTypePostgres, redis: config.CacheTypeMemory, wantNil: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Database.Type = tc.dbType
			cfg.Redis.Type = tc.redis

			guard, err := ValidateSingleInstanceGuard(cfg)
			require.NoError(t, err)
			require.Nil(t, guard, "PostgreSQL uses a database deployment guard")
			require.NoError(t, ReleaseSingleInstanceGuard(guard))
		})
	}
}

func TestValidateSingleInstanceGuardNilConfig(t *testing.T) {
	guard, err := ValidateSingleInstanceGuard(nil)
	require.NoError(t, err)
	require.Nil(t, guard)
}

func TestValidateSingleInstanceGuardAcquiresAndReleases(t *testing.T) {
	cfg := sqliteMemoryConfig(t)

	guard, err := ValidateSingleInstanceGuard(cfg)
	require.NoError(t, err)
	require.NotNil(t, guard)

	lockPath, err := singleInstanceLockPath(cfg)
	require.NoError(t, err)

	// The lock file exists and records this process id.
	data, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), pid)

	require.NoError(t, ReleaseSingleInstanceGuard(guard))
	require.FileExists(t, lockPath)
	next, err := acquireSingleInstanceLock(lockPath)
	require.NoError(t, err)
	require.NoError(t, ReleaseSingleInstanceGuard(next))
}

func TestValidateSingleInstanceGuardRejectsSecondInstance(t *testing.T) {
	cfg := sqliteMemoryConfig(t)
	lockPath, err := singleInstanceLockPath(cfg)
	require.NoError(t, err)

	other, err := acquireSingleInstanceLock(lockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.file.Close() })

	guard, err := ValidateSingleInstanceGuard(cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSingleInstanceViolation)
	require.Nil(t, guard)
}

func TestValidateSingleInstanceGuardTakesOverStaleLock(t *testing.T) {
	cfg := sqliteMemoryConfig(t)
	lockPath, err := singleInstanceLockPath(cfg)
	require.NoError(t, err)

	// A crashed process leaves the lock behind. Use a PID that is very unlikely
	// to be alive and verify it is not.
	stalePID := findDeadPID(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte(strconv.Itoa(stalePID)), 0o644))
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	guard, err := ValidateSingleInstanceGuard(cfg)
	require.NoError(t, err, "a stale lock from a dead process must be reclaimable")
	require.NotNil(t, guard)

	data, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), pid, "lock ownership moves to the current process")

	require.NoError(t, ReleaseSingleInstanceGuard(guard))
}

func TestValidateSingleInstanceGuardIsIdempotentWithinProcess(t *testing.T) {
	cfg := sqliteMemoryConfig(t)

	guard, err := ValidateSingleInstanceGuard(cfg)
	require.NoError(t, err)
	require.NotNil(t, guard)
	t.Cleanup(func() { _ = ReleaseSingleInstanceGuard(guard) })

	// A second call from the same process must not report a false conflict.
	second, err := ValidateSingleInstanceGuard(cfg)
	require.NoError(t, err)
	require.Nil(t, second)
}

func TestValidateSingleInstanceGuardIgnoresUnlockedCorruptMetadata(t *testing.T) {
	cfg := sqliteMemoryConfig(t)
	lockPath, err := singleInstanceLockPath(cfg)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte("not-a-pid"), 0o644))
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	guard, err := ValidateSingleInstanceGuard(cfg)
	require.NoError(t, err)
	require.NotNil(t, guard)
	require.NoError(t, ReleaseSingleInstanceGuard(guard))
}

func TestSingleInstanceLockPathNormalization(t *testing.T) {
	t.Run("defaults when path is empty", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Database.Type = config.DatabaseTypeSQLite

		path, err := singleInstanceLockPath(cfg)
		require.NoError(t, err)
		require.True(t, filepath.IsAbs(path))
		require.Equal(t, ".lock", filepath.Ext(path))
	})

	t.Run("preserves filename characters", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Database.Type = config.DatabaseTypeSQLite
		cfg.Database.Path = filepath.Join(t.TempDir(), "state #1.db")

		path, err := singleInstanceLockPath(cfg)
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(filepath.ToSlash(path), "state #1.db.lock"),
			"unexpected lock path: %s", path)
	})
}

func TestSingleInstanceChildHelper(t *testing.T) {
	path := os.Getenv("SUB2API_TEST_INSTANCE_LOCK")
	if path == "" {
		return
	}
	guard, err := acquireSingleInstanceLock(path)
	if err != nil {
		fmt.Fprintln(os.Stdout, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "locked")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
	// Intentionally skip Release: process exit must release the OS lock.
	_ = guard
	os.Exit(0)
}

func TestSingleInstanceLockAcrossProcessesAndExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSingleInstanceChildHelper$")
	cmd.Env = append(os.Environ(), "SUB2API_TEST_INSTANCE_LOCK="+path)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked", strings.TrimSpace(line))
	guard, err := acquireSingleInstanceLock(path)
	require.ErrorIs(t, err, ErrSingleInstanceViolation)
	require.Nil(t, guard)
	require.NoError(t, stdin.Close())
	require.NoError(t, cmd.Wait())
	guard, err = acquireSingleInstanceLock(path)
	require.NoError(t, err)
	require.NoError(t, ReleaseSingleInstanceGuard(guard))
}

func TestReleaseSingleInstanceGuardNilIsNoop(t *testing.T) {
	require.NoError(t, ReleaseSingleInstanceGuard(nil))
}

// findDeadPID returns a PID that is confirmed not to be running.
func findDeadPID(t *testing.T) int {
	t.Helper()

	for candidate := 100000; candidate < 200000; candidate++ {
		if !processAlive(candidate) {
			return candidate
		}
	}
	t.Skip("could not find a dead pid to use for the stale lock fixture")
	return 0
}
