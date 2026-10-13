package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteBatchImageJobTransitionsItemsAndSettlement(t *testing.T) {
	usage, userID, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	r := NewBatchImageRepository(usage.db)
	params := service.CreateBatchImageJobParams{UserID: userID, Provider: service.BatchImageProviderGeminiAPI, Model: "test-image", ItemCount: 1}
	job, err := r.CreateBatchImageJob(ctx, params)
	require.NoError(t, err)
	params.BatchID = job.BatchID
	_, err = r.CreateBatchImageJob(ctx, params)
	require.ErrorIs(t, err, service.ErrBatchImageJobExists)
	now := time.Now().UTC()
	err = r.TransitionBatchImageJobStatus(ctx, job.BatchID, service.BatchImageJobStatusUploading, service.BatchImageTransitionOptions{Now: &now, EventType: "status_changed"})
	require.NoError(t, err)
	loaded, err := r.GetBatchImageJobByBatchID(ctx, job.BatchID)
	require.NoError(t, err)
	require.Equal(t, service.BatchImageJobStatusUploading, loaded.Status)
	require.True(t, loaded.UpdatedAt.Equal(now))
	_, err = r.CreateBatchImageItem(ctx, service.CreateBatchImageItemParams{JobID: job.BatchID, CustomID: "line-1", Status: service.BatchImageItemStatusSuccess, ImageCount: 1})
	require.NoError(t, err)
	items, err := r.ListBatchImageItems(ctx, job.BatchID, service.BatchImageItemFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	_, err = usage.db.ExecContext(ctx, `UPDATE batch_image_jobs SET status='settling' WHERE batch_id=?`, job.BatchID)
	require.NoError(t, err)
	settlement := service.MarkBatchImageJobSettledParams{BatchID: job.BatchID, ActualCost: 0.25, ManifestHash: "test", Now: &now}
	require.NoError(t, r.MarkBatchImageJobSettled(ctx, settlement))
	require.ErrorIs(t, r.MarkBatchImageJobSettled(ctx, settlement), service.ErrBatchImageAlreadySettled)
	loaded, err = r.GetBatchImageJobByBatchID(ctx, job.BatchID)
	require.NoError(t, err)
	require.Equal(t, 0.25, *loaded.ActualCost)
}
