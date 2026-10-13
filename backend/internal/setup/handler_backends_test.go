package setup

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetupBackendValidation(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("SKIP_SETUP", "")
	r := gin.New()
	RegisterRoutes(r)
	for _, tc := range []struct {
		name, endpoint string
		body           map[string]any
		status         int
		message        string
	}{
		{"sqlite minimal", "test-db", map[string]any{"type": "sqlite", "path": "db/test.db"}, http.StatusOK, "Connection successful"},
		{"sqlite path required", "test-db", map[string]any{"type": "sqlite"}, http.StatusBadRequest, "path is required"},
		{"unknown database", "test-db", map[string]any{"type": "other"}, http.StatusBadRequest, "Invalid database type"},
		{"legacy database", "test-db", map[string]any{}, http.StatusBadRequest, "Invalid hostname"},
		{"memory minimal", "test-redis", map[string]any{"type": "memory"}, http.StatusOK, "no external service"},
		{"memory invalid capacity", "test-redis", map[string]any{"type": "memory", "memory": map[string]any{"max_capacity_mb": -1}}, http.StatusBadRequest, "must not be negative"},
		{"memory invalid gc", "test-redis", map[string]any{"type": "memory", "memory": map[string]any{"gc_interval_seconds": -1}}, http.StatusBadRequest, "must not be negative"},
		{"unknown cache", "test-redis", map[string]any{"type": "other"}, http.StatusBadRequest, "Invalid cache type"},
		{"legacy cache", "test-redis", map[string]any{}, http.StatusBadRequest, "Invalid hostname"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.body)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/setup/"+tc.endpoint, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.message)
		})
	}
}
