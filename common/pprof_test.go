package common

import (
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
