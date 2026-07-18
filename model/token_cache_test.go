package model

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateTokenCacheRetryStateForTest(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		tokenCacheRetries.Lock()
		defer tokenCacheRetries.Unlock()
		return !tokenCacheRetries.running && len(tokenCacheRetries.pending) == 0
	}, time.Second, 10*time.Millisecond)

	tokenCacheRetries.Lock()
	oldPending := tokenCacheRetries.pending
	oldRunning := tokenCacheRetries.running
	tokenCacheRetries.pending = make(map[string]*tokenCacheRetryState)
	tokenCacheRetries.running = true
	tokenCacheRetries.Unlock()

	t.Cleanup(func() {
		tokenCacheRetries.Lock()
		tokenCacheRetries.pending = oldPending
		tokenCacheRetries.running = oldRunning
		tokenCacheRetries.Unlock()
		select {
		case <-tokenCacheRetries.wake:
		default:
		}
	})
}

func TestTokenCacheDeleteRetryGetsBoundedDeadline(t *testing.T) {
	isolateTokenCacheRetryStateForTest(t)

	enqueueTokenCacheRetry("delete-deadline-token", true, errors.New("injected outage"))

	tokenCacheRetries.Lock()
	defer tokenCacheRetries.Unlock()
	state := tokenCacheRetries.pending[getTokenCacheKey("delete-deadline-token")]
	require.NotNil(t, state)
	assert.True(t, state.deleteEntry)
	assert.False(t, state.deadline.IsZero())
}

func TestTokenCacheDeleteRetryExpires(t *testing.T) {
	isolateTokenCacheRetryStateForTest(t)
	cacheKey := getTokenCacheKey("delete-expiry-token")
	versionKey := getTokenCacheVersionKey("delete-expiry-token")

	tokenCacheRetries.Lock()
	tokenCacheRetries.pending[cacheKey] = &tokenCacheRetryState{
		cacheKey:    cacheKey,
		versionKey:  versionKey,
		revision:    1,
		deleteEntry: true,
		cause:       errors.New("injected outage"),
		attempts:    3,
		delay:       tokenCacheRetryMaxDelay,
		nextAttempt: time.Now().Add(time.Hour),
		deadline:    time.Now().Add(-time.Millisecond),
	}
	tokenCacheRetries.Unlock()

	attempts, _, done := claimTokenCacheRetryAttempts()

	assert.Empty(t, attempts)
	assert.True(t, done)
	tokenCacheRetries.Lock()
	assert.Empty(t, tokenCacheRetries.pending)
	tokenCacheRetries.Unlock()
}
