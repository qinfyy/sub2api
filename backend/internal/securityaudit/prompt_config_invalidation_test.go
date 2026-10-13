package securityaudit

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalConfigInvalidationBusPublishAndUnsubscribe(t *testing.T) {
	bus := NewLocalConfigInvalidationBus()
	var received atomic.Int64
	cancel, err := bus.Subscribe(context.Background(), func(version int64) {
		received.Store(version)
	})
	require.NoError(t, err)
	require.NoError(t, bus.Publish(context.Background(), 7))
	require.Equal(t, int64(7), received.Load())

	cancel()
	require.NoError(t, bus.Publish(context.Background(), 8))
	require.Equal(t, int64(7), received.Load())
}

func TestLocalConfigInvalidationBusStopsWithContext(t *testing.T) {
	bus := NewLocalConfigInvalidationBus().(*localConfigInvalidationBus)
	ctx, cancel := context.WithCancel(context.Background())
	var received atomic.Int64
	_, err := bus.Subscribe(ctx, func(version int64) {
		received.Store(version)
	})
	require.NoError(t, err)
	cancel()
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subscribers) == 0
	}, time.Second, time.Millisecond)
	require.NoError(t, bus.Publish(context.Background(), 9))
	require.Equal(t, int64(0), received.Load())
}
