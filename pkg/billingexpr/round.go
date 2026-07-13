package billingexpr

import "github.com/MAX-API-Next/MAX-API/common"

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding. Every tiered billing path (pre-consume, settlement, breakdown
// validation, log fields) MUST use this function to avoid +-1 discrepancies.
func QuotaRound(f float64) int {
	return common.QuotaRound(f)
}

// QuotaRoundStrict rejects an unrepresentable pre-consume estimate.
func QuotaRoundStrict(f float64) (int, error) {
	return common.QuotaRoundStrict(f)
}
