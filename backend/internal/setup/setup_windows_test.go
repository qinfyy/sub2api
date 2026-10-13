//go:build windows

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWindowsSetupDirectoryAndInstallationMarker(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "working directory"
		if explicit {
			name = "explicit data directory"
		}
		t.Run(name, func(t *testing.T) {
			workDir := t.TempDir()
			t.Chdir(workDir)
			t.Setenv("DATA_DIR", "")
			t.Setenv("SKIP_SETUP", "")
			dataDir, want := workDir, "."
			if explicit {
				dataDir = t.TempDir()
				want = dataDir
				t.Setenv("DATA_DIR", dataDir)
			}
			require.Equal(t, want, GetDataDir())
			require.True(t, NeedsSetup())
			path, err := filepath.Abs(GetConfigFilePath())
			require.NoError(t, err)
			require.Equal(t, filepath.Join(dataDir, ConfigFileName), path)
			require.NoError(t, os.WriteFile(filepath.Join(dataDir, InstallLockFile), []byte("installed"), 0o600))
			require.False(t, NeedsSetup())
		})
	}
}

func TestWindowsInstallSQLiteMemoryInWorkingDirectory(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	for _, key := range []string{"DATA_DIR", "CONFIG_FILE", "SKIP_SETUP", "SERVER_HOST", "SERVER_PORT"} {
		t.Setenv(key, "")
	}
	cfg := &SetupConfig{
		Database: DatabaseConfig{Type: "sqlite", Path: "data/sub2api.db"},
		Redis:    RedisConfig{Type: "memory"},
		Admin:    AdminConfig{Email: "admin@example.com", Password: "test-password-123"},
		Server:   ServerConfig{Host: "127.0.0.1", Port: 18088, Mode: "release"},
		JWT:      JWTConfig{Secret: strings.Repeat("a", 32)},
		Timezone: "UTC",
	}
	require.True(t, NeedsSetup())
	require.NoError(t, Install(cfg))
	for _, path := range []string{ConfigFileName, InstallLockFile, filepath.Join("data", "sub2api.db")} {
		require.FileExists(t, filepath.Join(workDir, path))
	}
	require.False(t, NeedsSetup())
	require.Equal(t, "127.0.0.1:18088", config.GetServerAddress())

	db, err := openSetupSQLite(&cfg.Database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var role, passwordHash string
	require.NoError(t, db.QueryRow("SELECT role, password_hash FROM users WHERE email = ?", cfg.Admin.Email).Scan(&role, &passwordHash))
	require.Equal(t, service.RoleAdmin, role)
	user := &service.User{PasswordHash: passwordHash}
	require.True(t, user.CheckPassword(cfg.Admin.Password))
}
