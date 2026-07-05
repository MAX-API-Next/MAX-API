package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunWithTimeoutCompletes(t *testing.T) {
	assert.True(t, runWithTimeout(time.Second, func() {}))
}

func TestRunWithTimeoutTimesOut(t *testing.T) {
	start := time.Now()
	assert.False(t, runWithTimeout(10*time.Millisecond, func() {
		time.Sleep(50 * time.Millisecond)
	}))
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}
