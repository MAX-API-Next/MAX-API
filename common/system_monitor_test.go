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

func TestUpdateSystemStatusPreservesProbeValidity(t *testing.T) {
	originalClock := systemStatusClock
	originalCPUProbe := systemCPUUsageProbe
	originalMemoryProbe := systemMemoryUsageProbe
	originalDiskProbe := systemDiskUsageProbe
	originalStatus := GetSystemStatus()
	t.Cleanup(func() {
		systemStatusClock = originalClock
		systemCPUUsageProbe = originalCPUProbe
		systemMemoryUsageProbe = originalMemoryProbe
		systemDiskUsageProbe = originalDiskProbe
		latestSystemStatus.Store(originalStatus)
	})

	observedAt := time.Unix(1000, 0)
	systemStatusClock = func() time.Time { return observedAt }
	systemCPUUsageProbe = func() (float64, bool) { return 0, false }
	systemMemoryUsageProbe = func() (float64, bool) { return 72.5, true }
	systemDiskUsageProbe = func() (float64, bool) { return 0, false }

	updateSystemStatus()
	status := GetSystemStatus()

	require.Equal(t, observedAt, status.ObservedAt)
	require.False(t, status.CPUValid)
	require.Zero(t, status.CPUUsage)
	require.True(t, status.MemoryValid)
	require.Equal(t, 72.5, status.MemoryUsage)
	require.False(t, status.DiskValid)
	require.Zero(t, status.DiskUsage)
}
