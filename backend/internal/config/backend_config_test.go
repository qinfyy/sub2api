package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultConfigIsIsolatedAndValidForBackends(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("database.type", "do-not-read")
	t.Setenv("DATABASE_TYPE", "also-do-not-read")
	for _, database := range []string{DatabaseTypePostgres, DatabaseTypeSQLite} {
		for _, cache := range []string{CacheTypeRedis, CacheTypeMemory} {
			cfg := NewDefaultConfig()
			require.Equal(t, DatabaseTypePostgres, cfg.Database.Type)
			cfg.JWT.Secret = strings.Repeat("a", 32)
			cfg.Database.Type = database
			cfg.Redis.Type = cache
			require.NoError(t, cfg.Validate(), "%s + %s", database, cache)
		}
	}
	require.Equal(t, "do-not-read", viper.GetString("database.type"))
}

func TestLoadBackendSelectionRejectsUnknownTypes(t *testing.T) {
	for _, key := range []string{"DATABASE_TYPE", "REDIS_TYPE"} {
		t.Run(key, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv(key, "typo")
			_, err := Load()
			require.Error(t, err)
			require.Contains(t, err.Error(), "typo")
		})
	}
}

func TestLoadSQLitePathRelativeToConfig(t *testing.T) {
	resetViperWithJWTSecret(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("database:\n  type: sqlite\n  path: nested/state.db\nredis:\n  type: memory\n"), 0o600))
	t.Setenv("CONFIG_FILE", configPath)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "nested", "state.db"), cfg.Database.Path)
}

func TestSQLiteDSNEscapesFilenameAndUsesImmediateTransactions(t *testing.T) {
	cfg := DatabaseConfig{Path: "data/a #?%.db"}
	u, err := url.Parse(cfg.SQLiteDSN())
	require.NoError(t, err)
	require.Equal(t, "file", u.Scheme)
	path := u.Path
	if path == "" {
		path, err = url.PathUnescape(u.Opaque)
		require.NoError(t, err)
	}
	require.Equal(t, cfg.Path, path)
	require.Equal(t, "immediate", u.Query().Get("_txlock"))
	require.ElementsMatch(t, []string{"busy_timeout(10000)", "journal_mode(WAL)", "foreign_keys(1)"}, u.Query()["_pragma"])
}
