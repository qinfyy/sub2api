package repository

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newMemoryPersistenceDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "kv.db"))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE kv_store (
		key TEXT PRIMARY KEY, value BLOB NOT NULL, expires_at INTEGER, updated_at TEXT NOT NULL
	)`)
	require.NoError(t, err)
	return db
}

func TestMemoryPluginKVPersistsRawBytesAndListsWithoutWarmup(t *testing.T) {
	db := newMemoryPersistenceDB(t)
	ctx := context.Background()
	core := newMemoryCacheCore(1)
	store := NewMemoryPluginKVStore(core, db)
	value := []byte{0, 1, 255, '"', '\\'}
	require.NoError(t, store.Set(ctx, "plugin", "ns", "first", value, 0))
	got, found, err := store.Get(ctx, "plugin", "ns", "first")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, value, got)
	require.NoError(t, store.Set(ctx, "plugin", "ns", "second", []byte("second"), 0))
	require.NoError(t, store.Set(ctx, "other", "ns", "other", []byte("other"), 0))

	for _, current := range []service.PluginKVStore{store, NewMemoryPluginKVStore(newMemoryCacheCore(1), db)} {
		keys, err := current.List(ctx, "plugin", "ns", "", 10)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"first", "second"}, keys)
		got, found, err := current.Get(ctx, "plugin", "ns", "first")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, value, got)
	}
}

func TestMemoryPersistenceFailedWriteDoesNotChangeVisibleValue(t *testing.T) {
	db := newMemoryPersistenceDB(t)
	ctx := context.Background()
	core := newMemoryCacheCore(10)
	store := NewMemoryImageTaskStore(core, db)
	require.NoError(t, store.Save(ctx, &service.ImageTaskRecord{ID: "task"}, time.Hour))
	require.NoError(t, db.Close())
	require.Error(t, store.Save(ctx, &service.ImageTaskRecord{ID: "new"}, time.Hour))
	_, cached := core.get(imageTaskKey("new"))
	require.False(t, cached, "a failed durable write must not become visible in memory")
	_, err := store.Get(ctx, "task")
	require.Error(t, err, "durable state must fail closed when SQL is unavailable")
}

func TestMemoryRefreshTokenSetsConcurrentAndRevocation(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(fmt.Sprint(durable), func(t *testing.T) {
			var db *sql.DB
			if durable {
				db = newMemoryPersistenceDB(t)
			}
			ctx := context.Background()
			cache := NewMemoryRefreshTokenCache(newMemoryCacheCore(100), db)
			var wg sync.WaitGroup
			for i := 0; i < 24; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					hash := fmt.Sprint(i)
					if err := cache.StoreRefreshToken(ctx, hash, &service.RefreshTokenData{UserID: 7}, time.Hour); err != nil {
						t.Error(err)
					}
					if err := cache.AddToUserTokenSet(ctx, 7, hash, time.Hour); err != nil {
						t.Error(err)
					}
				}(i)
			}
			wg.Wait()
			if durable {
				cache = NewMemoryRefreshTokenCache(newMemoryCacheCore(1), db)
			}
			hashes, err := cache.GetUserTokenHashes(ctx, 7)
			require.NoError(t, err)
			require.Len(t, hashes, 24)
			require.NoError(t, cache.DeleteUserRefreshTokens(ctx, 7))
			for _, hash := range hashes {
				_, err := cache.GetRefreshToken(ctx, hash)
				require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
			}
		})
	}
}

func TestMemoryPasskeyConsumeAcrossIndependentCores(t *testing.T) {
	db := newMemoryPersistenceDB(t)
	ctx := context.Background()
	first := NewMemoryPasskeySessionStore(newMemoryCacheCore(10), db)
	token, err := first.Store(ctx, &service.PasskeySession{UserID: 7, Kind: "login"}, time.Hour)
	require.NoError(t, err)
	secondCore := newMemoryCacheCore(10)
	require.NoError(t, WarmMemoryKVStore(ctx, secondCore, db))
	second := NewMemoryPasskeySessionStore(secondCore, db)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, store := range []service.PasskeySessionStore{first, second} {
		wg.Add(1)
		go func(store service.PasskeySessionStore) {
			defer wg.Done()
			if _, err := store.Consume(ctx, token); err == nil {
				wins.Add(1)
			}
		}(store)
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
}

func TestMemoryRefreshTokenSetKeepsLongestLifetime(t *testing.T) {
	db := newMemoryPersistenceDB(t)
	cache := NewMemoryRefreshTokenCache(newMemoryCacheCore(1), db)
	ctx := context.Background()
	require.NoError(t, cache.AddToFamilyTokenSet(ctx, "family", "long", time.Hour))
	var before, after int64
	require.NoError(t, db.QueryRow("SELECT expires_at FROM kv_store WHERE key = ?", tokenFamilyKey("family")).Scan(&before))
	require.NoError(t, cache.AddToFamilyTokenSet(ctx, "family", "short", time.Second))
	require.NoError(t, db.QueryRow("SELECT expires_at FROM kv_store WHERE key = ?", tokenFamilyKey("family")).Scan(&after))
	require.GreaterOrEqual(t, after, before)
}

func TestMemoryPersistentStoresEnforceExpiryAndLiteralPrefixes(t *testing.T) {
	db := newMemoryPersistenceDB(t)
	ctx := context.Background()
	store := NewMemoryPluginKVStore(newMemoryCacheCore(1), db)
	for _, key := range []string{"a_one", "abone", "a_two"} {
		require.NoError(t, store.Set(ctx, "plugin", "ns", key, []byte(key), time.Hour))
	}
	_, err := db.Exec("UPDATE kv_store SET expires_at = 1 WHERE key = ?", pluginKVKeyPrefix+"plugin:ns:a_two")
	require.NoError(t, err)
	keys, err := store.List(ctx, "plugin", "ns", "a_", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"a_one"}, keys)
	_, found, err := store.Get(ctx, "plugin", "ns", "a_two")
	require.NoError(t, err)
	require.False(t, found)

	passkeys := NewMemoryPasskeySessionStore(newMemoryCacheCore(1), db)
	token, err := passkeys.Store(ctx, &service.PasskeySession{UserID: 1, Kind: "login"}, time.Hour)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE kv_store SET expires_at = 1 WHERE key = ?", passkeySessionPrefix+token)
	require.NoError(t, err)
	_, err = passkeys.Consume(ctx, token)
	require.ErrorIs(t, err, service.ErrPasskeySession)
}
