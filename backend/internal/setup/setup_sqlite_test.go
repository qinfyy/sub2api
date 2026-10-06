package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestInstallSQLiteMemoryCreatesAdminAndConfig(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("SKIP_SETUP", "")
	cfg := &SetupConfig{
		Database: DatabaseConfig{Type: " SQLite ", Path: "nested/state #1.db"},
		Redis:    RedisConfig{Type: " Memory "},
		Admin:    AdminConfig{Email: "admin@example.com", Password: "test-password-123"},
		JWT:      JWTConfig{Secret: strings.Repeat("a", 32)},
		Timezone: "UTC",
	}
	require.NoError(t, Install(cfg))
	_, err := os.Stat(filepath.Join(dataDir, "nested", "state #1.db"))
	require.NoError(t, err)
	require.False(t, NeedsSetup())

	db, err := openSetupSQLite(&cfg.Database)
	require.NoError(t, err)
	defer db.Close()
	var role, passwordHash string
	require.NoError(t, db.QueryRow("SELECT role, password_hash FROM users WHERE email = ?", cfg.Admin.Email).Scan(&role, &passwordHash))
	require.Equal(t, service.RoleAdmin, role)
	require.NotEqual(t, cfg.Admin.Password, passwordHash)
	user := &service.User{PasswordHash: passwordHash}
	require.True(t, user.CheckPassword(cfg.Admin.Password))
	created, reason, err := createAdminUser(cfg)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, adminBootstrapReasonAdminExists, reason)

	contents, err := os.ReadFile(GetConfigFilePath())
	require.NoError(t, err)
	var persisted SetupConfig
	require.NoError(t, yaml.Unmarshal(contents, &persisted))
	require.Equal(t, "sqlite", persisted.Database.Type)
	require.Equal(t, "memory", persisted.Redis.Type)
	require.Positive(t, persisted.Redis.Memory.MaxCapacityMB)
	require.Positive(t, persisted.Redis.Memory.GCIntervalSeconds)
	require.Equal(t, cfg.Database.Path, persisted.Database.Path)
	require.Error(t, Install(cfg))
}
