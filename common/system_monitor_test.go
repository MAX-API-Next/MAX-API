package common

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSystemMonitorCanStopAndRestart(t *testing.T) {
	for i := 0; i < 2; i++ {
		StartSystemMonitor()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		require.NoError(t, StopSystemMonitor(ctx))
		cancel()
	}
}
