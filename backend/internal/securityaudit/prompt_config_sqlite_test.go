package securityaudit

import (
	"context"
	"sync"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestSQLiteAuditConfigCASAndSecretRestart(t *testing.T) {
	client, db := openPromptSQLiteBackend(t)
	settings := repository.NewSettingRepository(client)
	ctx := context.Background()
	require.NoError(t, settings.Set(ctx, SettingKeyRiskControl, "true"))
	encryptor := promptAuditTestEncryptor(t)
	m := NewConfigManager(db, settings, nil, encryptor, testTotpKeyConfig())
	t.Cleanup(func() { require.NoError(t, m.Shutdown(ctx)) })
	require.NoError(t, m.Reload(ctx))
	public, err := m.Save(ctx, promptAuditUpdateRequest(1, 1, "sqlite-secret-canary"), 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, public.ConfigVersion)
	raw, err := settings.GetValue(ctx, SettingKeyPromptAuditConfig)
	require.NoError(t, err)
	require.NotContains(t, raw, "sqlite-secret-canary")
	stored, err := ParseStorageConfig(raw)
	require.NoError(t, err)
	plain, err := encryptor.Decrypt(stored.Endpoints[0].TokenCiphertext)
	require.NoError(t, err)
	require.Equal(t, "sqlite-secret-canary", plain)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.Save(ctx, promptAuditUpdateRequest(2, i+2, ""), int64(i+2))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else {
			require.Equal(t, ErrorCodeConfigConflict, infraerrors.Reason(err))
			conflicted++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	reloaded := NewConfigManager(db, settings, nil, encryptor, testTotpKeyConfig())
	t.Cleanup(func() { require.NoError(t, reloaded.Shutdown(ctx)) })
	require.NoError(t, reloaded.Reload(ctx))
	active, ok := reloaded.Active()
	require.True(t, ok)
	require.EqualValues(t, 3, active.ConfigVersion)
}
