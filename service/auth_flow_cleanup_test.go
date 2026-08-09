package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunAuthFlowCleanupStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var calls atomic.Int32
	go func() {
		defer close(done)
		runAuthFlowCleanup(ctx, time.Millisecond, func() {
			calls.Add(1)
		})
	}()

	require.Eventually(t, func() bool { return calls.Load() >= 2 }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("auth flow cleanup did not stop after cancellation")
	}
}
