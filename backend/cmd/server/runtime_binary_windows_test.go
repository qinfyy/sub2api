package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Opt-in release smoke test. It only installs into a new temporary directory.
func TestWindowsRuntimeBinaryInstallAndRestart(t *testing.T) {
	binary := os.Getenv("SUB2API_RUNTIME_EXE")
	if binary == "" {
		t.Skip("set SUB2API_RUNTIME_EXE to the newly built executable")
	}
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	request := func(method, path string, body any) map[string]any {
		t.Helper()
		data, err := json.Marshal(body)
		require.NoError(t, err)
		req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(data))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		data, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, data)
		var value struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(data, &value))
		return value.Data
	}
	start := func(name, readyPath string) (*exec.Cmd, string) {
		t.Helper()
		logPath := filepath.Join(dir, name+".log")
		output, err := os.Create(logPath)
		require.NoError(t, err)
		cmd := exec.Command(binary)
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			key = strings.ToUpper(key)
			if key == "DATA_DIR" || key == "CONFIG_FILE" || key == "AUTO_SETUP" || key == "SKIP_SETUP" ||
				strings.HasPrefix(key, "SERVER_") || strings.HasPrefix(key, "DATABASE_") ||
				strings.HasPrefix(key, "REDIS_") || strings.HasPrefix(key, "PRICING_") {
				continue
			}
			cmd.Env = append(cmd.Env, entry)
		}
		cmd.Env = append(cmd.Env, "SERVER_HOST=127.0.0.1", "SERVER_PORT="+strconv.Itoa(port),
			"RUN_MODE=standard", "PRICING_REMOTE_URL=https://127.0.0.1:1/pricing.json",
			"PRICING_HASH_URL=https://127.0.0.1:1/pricing.sha256",
			"HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "NO_PROXY=localhost,127.0.0.1")
		cmd.Stdout, cmd.Stderr = output, output
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = output.Close() })
		ready := false
		until := time.Now().Add(45 * time.Second)
		for time.Now().Before(until) {
			resp, err := client.Get(baseURL + readyPath)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					ready = true
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !ready {
			content, _ := os.ReadFile(logPath)
			t.Fatalf("binary did not become ready: %s", content)
		}
		return cmd, logPath
	}
	setup, _ := start("setup", "/setup/status")
	require.Equal(t, true, request("GET", "/setup/status", nil)["needs_setup"])
	resp, err := client.Get(baseURL + "/")
	require.NoError(t, err)
	html, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	asset := regexp.MustCompile(`src="(/assets/[^\"]+\.js)"`).FindStringSubmatch(string(html))
	require.Len(t, asset, 2, "embedded frontend missing")
	resp, err = client.Get(baseURL + asset[1])
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	request("POST", "/setup/install", map[string]any{
		"database": map[string]any{"type": "sqlite", "path": "data/sub2api.db"},
		"redis":    map[string]any{"type": "memory", "memory": map[string]any{"max_capacity_mb": 64}},
		"admin":    map[string]any{"email": "binary@example.test", "password": "binary-smoke-test-password"},
		"server":   map[string]any{"host": "127.0.0.1", "port": port, "mode": "release"},
	})
	for _, name := range []string{"config.yaml", ".installed", "data/sub2api.db"} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "expected installation in working directory")
	}
	// Windows has no systemd restart. Re-launch explicitly, then crash/restart
	// once more to validate the installed marker, admin and SQLite WAL recovery.
	require.NoError(t, setup.Process.Kill())
	_ = setup.Wait()
	for i := 0; i < 2; i++ {
		server, logPath := start(fmt.Sprintf("server-%d", i), "/health")
		login := request("POST", "/api/v1/auth/login", map[string]any{"email": "binary@example.test", "password": "binary-smoke-test-password"})
		require.NotEmpty(t, login["access_token"])
		prices, err := os.ReadFile(filepath.Join(dir, "data", "model_pricing.json"))
		require.NoError(t, err)
		var models map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(prices, &models))
		require.Greater(t, len(models), 100)
		if i == 0 {
			content, err := os.ReadFile(logPath)
			require.NoError(t, err)
			require.Contains(t, string(content), "embedded default catalog")
		}
		require.NoError(t, server.Process.Kill())
		_ = server.Wait()
	}
	t.Log("BINARY ACCEPTANCE: CWD install, embedded frontend/assets, unavailable remote pricing fallback, login and crash/restart passed")
}
