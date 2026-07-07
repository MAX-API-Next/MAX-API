package common

import "math"

// QuotaFromFloat converts a computed quota value to int with saturation.
// Quota products can include user-controlled multipliers such as image count,
// video seconds, or resolution ratios; oversized products must never wrap into
// a negative charge. The bound is int32 because quota columns are int fields
// used as 32-bit database integers in supported deployments.
func QuotaFromFloat(value float64) int {
	if math.IsNaN(value) {
		return 0
	}
	if value >= math.MaxInt32 {
		return math.MaxInt32
	}
	if value <= math.MinInt32 {
		return math.MinInt32
	}
	return int(value)
}
