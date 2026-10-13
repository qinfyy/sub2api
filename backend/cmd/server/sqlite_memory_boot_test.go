package main

import (
	"os"
	"net"
	"strconv"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/alicebob/miniredis/v2"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestSQLiteMemoryApplicationBoot 验证 SQLite + 内存组合能完成完整的依赖注入
// 与服务构造（不监听端口），确保没有残留的 Redis / PostgreSQL 强制依赖。
func TestSQLiteMemoryApplicationBoot(t *testing.T) {
	app := newSQLiteTestApplication(t, "memory")
	require.True(t, app.Backends.MemoryCacheEnabled())
	require.Nil(t, app.Backends.Redis)
}

func newSQLiteTestApplication(t *testing.T, cache string) *Application {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DATA_DIR", dir)
	viper.Reset()
	t.Cleanup(viper.Reset)
	configPath := filepath.Join(dir, "config.yaml")
	redisPort := 6379
	if cache == "redis" {
		r := miniredis.RunT(t)
		_, port, err := net.SplitHostPort(r.Addr())
		require.NoError(t, err)
		redisPort, err = strconv.Atoi(port)
		require.NoError(t, err)
	}
	content := `
run_mode: standard
timezone: Asia/Shanghai
server:
  host: 127.0.0.1
  port: 18080
database:
  type: sqlite
  path: ` + filepath.ToSlash(filepath.Join(dir, "sub2api.db")) + `
redis:
  type: ` + cache + `
  host: 127.0.0.1
  port: ` + fmt.Sprint(redisPort) + `
  memory:
    max_capacity_mb: 64
    gc_interval_seconds: 60
    persistence_path: ` + filepath.ToSlash(filepath.Join(dir, "cache-state.db")) + `
jwt:
  secret: 0123456789abcdef0123456789abcdef
pricing:
  data_dir: data
  remote_url: ""
  hash_url: ""
plugins:
  data_dir: plugins
log:
  output:
    to_file: false
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "data"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data", "model_pricing.json"), []byte(`{"test-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`), 0o600))
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
	t.Setenv("CONFIG_FILE", configPath)

	app, cleanup, err := initializeApplication(handler.BuildInfo{Version: "test", BuildType: "test"})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NotNil(t, app)
	t.Cleanup(app.Cleanup)
	require.NotNil(t, app.Backends)
	return app
}
