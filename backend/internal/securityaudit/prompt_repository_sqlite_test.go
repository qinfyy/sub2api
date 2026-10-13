package securityaudit

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/stretchr/testify/require"
)

func openPromptSQLiteDB(t *testing.T) *sql.DB {
	_, db := openPromptSQLiteBackend(t)
	return db
}

func openPromptSQLiteBackend(t *testing.T) (*ent.Client, *sql.DB) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type, cfg.Database.Path = "sqlite", filepath.Join(t.TempDir(), "audit.db")
	cfg.Redis.Type, cfg.Timezone = "memory", "UTC"
	client, db, err := repository.InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client, db
}

func TestSQLiteAuditQueueLifecycleAndEventDeletion(t *testing.T) {
	db := openPromptSQLiteDB(t)
	r := NewPostgreSQLRepository(db)
	ctx := context.Background()
	snapshot := integrationSnapshot("sqlite")
	job, err := r.CreateStagingWithCapacity(ctx, snapshot, 1, 3, 1)
	require.NoError(t, err)
	_, err = r.CreateStagingWithCapacity(ctx, snapshot, 1, 3, 1)
	require.ErrorIs(t, err, ErrQueueFull)
	require.NoError(t, r.PublishQueued(ctx, job.ID))
	claimed, ok, err := r.ClaimNextJob(ctx, time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, job.ID, claimed.ID)
	require.NoError(t, r.RefreshLease(ctx, claimed.ID, claimed.ClaimVersion, time.Now()))
	_, ok, err = r.ClaimNextJob(ctx, time.Now().Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok)
	event, err := r.Complete(ctx, claimed, integrationResult(EventCritical), true)
	require.NoError(t, err)
	_, err = r.Complete(ctx, claimed, integrationResult(EventCritical), true)
	require.ErrorIs(t, err, ErrLeaseLost)
	loaded, err := r.GetEvent(ctx, event.ID)
	require.NoError(t, err)
	require.Equal(t, EventCritical, loaded.Decision)
	start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	filter := EventFilter{StartAt: &start, EndAt: &end, Keyword: "SQLITE"}
	page, err := r.ListEvents(ctx, filter, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	preview, err := r.PreviewDelete(ctx, filter)
	require.NoError(t, err)
	deleted, err := r.DeleteEventsByFilter(ctx, filter, preview.SnapshotMaxID, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted.DeletedEvents)
	require.EqualValues(t, 1, deleted.DeletedJobs)
	_, err = r.GetEvent(ctx, event.ID)
	require.ErrorIs(t, err, ErrEventNotFound)
	blocking, err := r.RecordBlocking(ctx, snapshot, 1, integrationResult(EventFlag), false)
	require.NoError(t, err)
	deleted, err = r.DeleteEventsByIDs(ctx, []int64{blocking.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted.DeletedJobs)
}

func TestSQLiteAuditConcurrentClaimAndExpiredLease(t *testing.T) {
	r := NewPostgreSQLRepository(openPromptSQLiteDB(t))
	ctx := context.Background()
	job, err := r.CreateStagingWithCapacity(ctx, integrationSnapshot("lease"), 1, 2, 10)
	require.NoError(t, err)
	require.NoError(t, r.PublishQueued(ctx, job.ID))
	claimed := make(chan *Job, 12)
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, ok, err := r.ClaimNextJob(ctx, time.Now().Add(time.Second))
			if err != nil {
				errs <- err
			} else if ok {
				claimed <- value
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, claimed, 1)
	old := <-claimed
	n, err := r.ReclaimStale(ctx, time.Now().Add(time.Hour), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	fresh, ok, err := r.ClaimNextJob(ctx, time.Now().Add(2*time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, fresh.ClaimVersion, old.ClaimVersion)
	_, err = r.Complete(ctx, old, integrationResult(EventPass), true)
	require.ErrorIs(t, err, ErrLeaseLost)
	require.NoError(t, r.Fail(ctx, fresh.ID, fresh.ClaimVersion, "test", ""))
}
