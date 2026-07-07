package billingexpr

import "math"

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding. Every tiered billing path (pre-consume, settlement, breakdown
// validation, log fields) MUST use this function to avoid +-1 discrepancies.
//
// The result saturates at int32 bounds: quota columns are 32-bit integers in
// the database, and oversized expression results must never wrap around.
func QuotaRound(f float64) int {
	r := math.Round(f)
	if math.IsNaN(r) {
		return 0
	}
	if r >= math.MaxInt32 {
		return math.MaxInt32
	}
	if r <= math.MinInt32 {
		return math.MinInt32
	}
	return int(r)
}
