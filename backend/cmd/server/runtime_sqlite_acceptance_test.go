package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Set SUB2API_RUNTIME_SOAK=30m for the release gate. The default is a short
// integration regression, using the real router, workers, migrations and DB.
func TestSQLiteMemoryRuntimeAcceptance(t *testing.T) {
	app := newSQLiteTestApplication(t, "memory")
	ctx := context.Background()
	user := &service.User{}
	require.NoError(t, user.SetPassword("runtime-test-password"))
	admin, err := app.Backends.Ent.User.Create().SetEmail("runtime@example.test").
		SetUsername("runtime").SetPasswordHash(user.PasswordHash).SetRole("admin").
		SetBalance(1000).SetConcurrency(5).Save(ctx)
	require.NoError(t, err)
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"test-model","reasoning":false,"input_modalities":["text"],"context_window":128000}]}`)
		case "/v1/sub2api/billing":
			_, _ = io.WriteString(w, `{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","group_rate_multiplier":1,"resolved_rate_multiplier":1,"peak_rate_enabled":false,"effective_rate_multiplier":1,"observed_at":"2026-10-11T00:00:00Z"}`)
		case "/v1/chat/completions":
			n := upstreamCalls.Add(1)
			_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-test-%d","object":"chat.completion","created":1791676800,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`, n)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	server := httptest.NewServer(app.Server.Handler)
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(method, path, token string, payload any) map[string]any {
		t.Helper()
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		require.NoError(t, err, "%s %s", method, path)
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s %s: %s", method, path, data)
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(data, &envelope))
		if data, ok := envelope["data"].(map[string]any); ok {
			return data
		}
		return envelope
	}
	login := request("POST", "/api/v1/auth/login", "", map[string]any{"email": "runtime@example.test", "password": "runtime-test-password"})
	token, ok := login["access_token"].(string)
	require.True(t, ok)
	request("POST", "/api/v1/admin/compliance/accept", token, map[string]any{"phrase": service.AdminComplianceAckPhraseEN, "language": "en"})
	request("POST", "/api/v1/admin/users", token, map[string]any{"email": "second@example.test", "password": "second-test-password", "concurrency": 2})
	group := request("POST", "/api/v1/admin/groups", token, map[string]any{"name": "runtime", "platform": "openai", "rate_multiplier": 1, "subscription_type": "standard"})
	groupID := int64(group["id"].(float64))
	account := request("POST", "/api/v1/admin/accounts", token, map[string]any{
		"name": "runtime", "platform": "openai", "type": "apikey", "concurrency": 5, "rate_multiplier": 1, "group_ids": []int64{groupID},
		"credentials": map[string]any{"api_key": "test-key", "base_url": upstream.URL, "api_protocol": "chat_completions"},
		"extra":       map[string]any{"upstream_billing_probe_enabled": true, "upstream_billing_rate_sync_enabled": true},
	})
	accountID := int64(account["id"].(float64))
	request("POST", fmt.Sprintf("/api/v1/admin/accounts/%d/models/sync-upstream", accountID), token, map[string]any{})
	probePath := fmt.Sprintf("/api/v1/admin/accounts/%d/upstream-billing-probe", accountID)
	probe := request("POST", probePath, token, map[string]any{})
	require.Equal(t, "ok", probe["snapshot"].(map[string]any)["status"])
	key := request("POST", "/api/v1/keys", token, map[string]any{"name": "runtime", "group_id": groupID})
	apiKey := key["key"].(string)
	duration := 2 * time.Second
	if raw := os.Getenv("SUB2API_RUNTIME_SOAK"); raw != "" {
		duration, err = time.ParseDuration(raw)
		require.NoError(t, err)
	}
	started := time.Now()
	var calls int
	var maxLatency time.Duration
	for time.Since(started) < duration {
		at := time.Now()
		request("POST", "/v1/chat/completions", apiKey, map[string]any{"model": "test-model", "messages": []any{map[string]any{"role": "user", "content": "ping"}}, "stream": false})
		calls++
		if latency := time.Since(at); latency > maxLatency {
			maxLatency = latency
		}
		if calls%20 == 1 {
			request("POST", probePath, token, map[string]any{})
			for _, path := range []string{"/api/v1/auth/me", "/api/v1/admin/compliance", "/api/v1/admin/users", "/api/v1/admin/accounts", "/api/v1/admin/audit-logs", "/api/v1/admin/usage", "/api/v1/admin/usage/stats", "/api/v1/admin/dashboard/snapshot-v2", "/api/v1/usage", "/api/v1/subscriptions/active"} {
				request("GET", path, token, nil)
			}
		}
		if calls%120 == 0 {
			t.Logf("soak elapsed=%s calls=%d db=%+v", time.Since(started).Round(time.Second), calls, app.Backends.SQL.Stats())
		}
		time.Sleep(500 * time.Millisecond)
	}
	var rows int
	require.Eventually(t, func() bool {
		err := app.Backends.SQL.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE user_id=?`, admin.ID).Scan(&rows)
		return err == nil && rows == calls
	}, 10*time.Second, 100*time.Millisecond)
	var balance, cost float64
	require.NoError(t, app.Backends.SQL.QueryRow(`SELECT balance FROM users WHERE id=?`, admin.ID).Scan(&balance))
	require.NoError(t, app.Backends.SQL.QueryRow(`SELECT COALESCE(SUM(actual_cost),0) FROM usage_logs WHERE user_id=?`, admin.ID).Scan(&cost))
	require.Greater(t, cost, float64(0))
	require.InDelta(t, 1000-cost, balance, 0.000001)
	require.EqualValues(t, calls, upstreamCalls.Load())
	t.Logf("ACCEPTANCE elapsed=%s gateway_calls=%d usage_rows=%d cost=%.8f balance=%.8f max_latency=%s", time.Since(started), calls, rows, cost, balance, maxLatency)
}
