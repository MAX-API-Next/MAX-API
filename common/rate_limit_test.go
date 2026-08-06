package common

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInMemoryRateLimiterConcurrentInitAndStop(t *testing.T) {
	var limiter InMemoryRateLimiter
	var wg sync.WaitGroup
	errCh := make(chan error, 64)

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			limiter.Init(10 * time.Millisecond)
			if !limiter.Request(fmt.Sprintf("key-%d", index), 1, 1) {
				errCh <- fmt.Errorf("first request for key %d was rejected", index)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, limiter.Stop(ctx))
}
