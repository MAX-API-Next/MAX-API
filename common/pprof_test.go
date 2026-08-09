package common

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCaptureHighCPUProfileHandlesSamplingFailureAndEmptyResult(t *testing.T) {
	original := cpuPercent
	t.Cleanup(func() { cpuPercent = original })

	cpuPercent = func(time.Duration, bool) ([]float64, error) {
		return nil, errors.New("sample failed")
	}
	require.NotPanics(t, captureHighCPUProfile)

	cpuPercent = func(time.Duration, bool) ([]float64, error) {
		return nil, nil
	}
	require.NotPanics(t, captureHighCPUProfile)
}

func TestMonitorStopsOnCancellation(t *testing.T) {
	original := cpuPercent
	t.Cleanup(func() { cpuPercent = original })
	sampled := make(chan struct{}, 1)
	cpuPercent = func(time.Duration, bool) ([]float64, error) {
		select {
		case sampled <- struct{}{}:
		default:
		}
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		MonitorContext(ctx)
	}()

	select {
	case <-sampled:
	case <-time.After(time.Second):
		t.Fatal("CPU monitor did not sample")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CPU monitor did not stop after cancellation")
	}
}
