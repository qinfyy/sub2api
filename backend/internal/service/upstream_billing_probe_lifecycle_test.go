package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingProbeRepository struct {
	*upstreamBillingProbeAccountRepo
	entered chan struct{}
	release chan struct{}
}

func (r *blockingProbeRepository) GetByID(ctx context.Context, id int64) (*Account, error) {
	close(r.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return r.upstreamBillingProbeAccountRepo.GetByID(ctx, id)
	}
}

func TestUpstreamBillingProbeWaiterCancellation(t *testing.T) {
	r := &blockingProbeRepository{
		upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
			ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "test", "base_url": "https://example.test"},
		}}},
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	svc := NewUpstreamBillingProbeService(r, nil, nil)
	defer svc.Stop()
	done := make(chan error, 1)
	go func() { _, err := svc.ProbeAccount(context.Background(), 1); done <- err }()
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := svc.ProbeAccount(ctx, 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	select {
	case <-done:
		t.Fatal("waiter canceled the owner")
	default:
	}
	close(r.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("owner did not finish")
	}
}

func TestUpstreamBillingProbeStopCancelsOperations(t *testing.T) {
	r := &blockingProbeRepository{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{}, entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewUpstreamBillingProbeService(r, nil, nil)
	done := make(chan error, 1)
	go func() { _, err := svc.ProbeAccount(context.Background(), 1); done <- err }()
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
	require.ErrorIs(t, <-done, context.Canceled)
	_, err := svc.ProbeAccount(context.Background(), 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestUpstreamBillingProbeQueueAndContextBounds(t *testing.T) {
	svc := NewUpstreamBillingProbeService(nil, nil, nil)
	defer svc.Stop()
	ctx, cancel := svc.probeContext(context.Background())
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.InDelta(t, 20, time.Until(deadline).Seconds(), 0.1)
	cancel()
	for i := 0; i < cap(svc.probeSlots); i++ {
		svc.probeSlots <- struct{}{}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := svc.probeAccountWithMode(ctx, 1, 30, false)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
