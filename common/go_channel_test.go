package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSafeSendStringTimeoutReportsSendOutcome(t *testing.T) {
	t.Run("sent", func(t *testing.T) {
		ch := make(chan string, 1)
		require.True(t, SafeSendStringTimeout(ch, "value", 1))
		require.Equal(t, "value", <-ch)
	})

	t.Run("timeout", func(t *testing.T) {
		ch := make(chan string)
		started := time.Now()
		require.False(t, SafeSendStringTimeout(ch, "value", 0))
		require.Less(t, time.Since(started), time.Second)
	})

	t.Run("closed", func(t *testing.T) {
		ch := make(chan string)
		close(ch)
		require.False(t, SafeSendStringTimeout(ch, "value", 1))
	})
}
