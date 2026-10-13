package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLegacyMemoryStateIsRejectedBeforeOpeningPrimary(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-journal"} {
		t.Run("source"+suffix, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Redis.Type = config.CacheTypeMemory
			cfg.Redis.Memory.PersistencePath = filepath.Join(t.TempDir(), "old-state.db")
			path := cfg.Redis.Memory.PersistencePath + suffix
			original := []byte("state that must not be abandoned or changed")
			require.NoError(t, os.WriteFile(path, original, 0600))
			resources, cleanup, err := ProvideBackendResources(cfg)
			require.ErrorIs(t, err, ErrLegacyMemoryState)
			require.Nil(t, resources)
			require.Nil(t, cleanup)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, got)
			require.NoFileExists(t, cfg.Redis.Memory.PersistencePath+".lock")
		})
	}
}

func TestLegacyMemoryStateGuardDoesNotCreateOrOpenFiles(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Redis.Type = config.CacheTypeMemory
	cfg.Redis.Memory.PersistencePath = filepath.Join(t.TempDir(), "missing", "state.db")
	require.NoError(t, rejectLegacyMemoryState(cfg))
	require.NoDirExists(t, filepath.Dir(cfg.Redis.Memory.PersistencePath))
	cfg.Redis.Memory.PersistencePath = filepath.Join(t.TempDir(), "empty.db")
	require.NoError(t, os.WriteFile(cfg.Redis.Memory.PersistencePath, nil, 0600))
	require.NoError(t, rejectLegacyMemoryState(cfg))
	info, err := os.Stat(cfg.Redis.Memory.PersistencePath)
	require.NoError(t, err)
	require.Zero(t, info.Size())
	require.NoFileExists(t, cfg.Redis.Memory.PersistencePath+"-wal")

	require.NoError(t, os.WriteFile(cfg.Redis.Memory.PersistencePath, []byte("primary"), 0600))
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = cfg.Redis.Memory.PersistencePath
	require.NoError(t, rejectLegacyMemoryState(cfg), "SQLite already used its primary database")
	cfg.Database.Type = config.DatabaseTypePostgres
	cfg.Redis.Type = config.CacheTypeRedis
	require.NoError(t, rejectLegacyMemoryState(cfg), "Redis mode did not use the memory sidecar")
}
