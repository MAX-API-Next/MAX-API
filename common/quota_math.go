package common

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

// Quota conversions are centralized here so every billing path shares one
// saturation + logging policy. Quota columns are 32-bit integers in supported
// deployments, so an oversized product must clamp to the int32 range instead
// of wrapping around and turning a charge into a credit.
const (
	MaxQuota = math.MaxInt32
	MinQuota = math.MinInt32
)

// QuotaClampKind identifies why a quota conversion had to be saturated.
type QuotaClampKind string

const (
	QuotaClampOverflow  QuotaClampKind = "overflow"
	QuotaClampUnderflow QuotaClampKind = "underflow"
	QuotaClampNaN       QuotaClampKind = "nan"
)

// QuotaClamp describes a single saturation event and is stored under
// admin_info.quota_saturation for audit when a billing path has to clamp.
type QuotaClamp struct {
	Op       string         `json:"op"`
	Kind     QuotaClampKind `json:"kind"`
	Original float64        `json:"original"`
	Clamped  int            `json:"clamped"`
}

func (c *QuotaClamp) Error() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("quota conversion (%s) %s: original=%g, clamped=%d", c.Op, c.Kind, c.Original, c.Clamped)
}

func (c *QuotaClamp) AuditMap() map[string]interface{} {
	if c == nil {
		return nil
	}
	original := interface{}(c.Original)
	switch {
	case math.IsNaN(c.Original):
		original = "NaN"
	case math.IsInf(c.Original, 1):
		original = "+Inf"
	case math.IsInf(c.Original, -1):
		original = "-Inf"
	}
	return map[string]interface{}{
		"op":       c.Op,
		"kind":     c.Kind,
		"original": original,
		"clamped":  c.Clamped,
	}
}

func saturateQuota(value float64, op string) (int, *QuotaClamp) {
	var clamp *QuotaClamp
	switch {
	case math.IsNaN(value):
		clamp = &QuotaClamp{Op: op, Kind: QuotaClampNaN, Original: value, Clamped: 0}
	case value > MaxQuota:
		clamp = &QuotaClamp{Op: op, Kind: QuotaClampOverflow, Original: value, Clamped: MaxQuota}
	case value < MinQuota:
		clamp = &QuotaClamp{Op: op, Kind: QuotaClampUnderflow, Original: value, Clamped: MinQuota}
	default:
		return int(value), nil
	}
	SysError(clamp.Error())
	return clamp.Clamped, clamp
}

func strictQuota(quota int, clamp *QuotaClamp) (int, error) {
	if clamp != nil {
		return 0, clamp
	}
	return quota, nil
}

// QuotaFromFloat converts a computed quota value to int, truncating toward
// zero, with int32 saturation.
func QuotaFromFloat(value float64) int {
	quota, _ := QuotaFromFloatChecked(value)
	return quota
}

// QuotaFromFloatChecked is QuotaFromFloat plus a non-nil clamp descriptor
// when saturation or NaN fallback happened.
func QuotaFromFloatChecked(value float64) (int, *QuotaClamp) {
	return saturateQuota(value, "QuotaFromFloat")
}

// QuotaFromFloatStrict rejects unrepresentable billing estimates instead of
// silently saturating them.
func QuotaFromFloatStrict(value float64) (int, error) {
	return strictQuota(QuotaFromFloatChecked(value))
}

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding, with the same saturation policy.
func QuotaRound(value float64) int {
	quota, _ := QuotaRoundChecked(value)
	return quota
}

func QuotaRoundChecked(value float64) (int, *QuotaClamp) {
	return saturateQuota(math.Round(value), "QuotaRound")
}

func QuotaRoundStrict(value float64) (int, error) {
	return strictQuota(QuotaRoundChecked(value))
}

// QuotaFromDecimal rounds a computed quota decimal before conversion.
func QuotaFromDecimal(d decimal.Decimal) int {
	quota, _ := QuotaFromDecimalChecked(d)
	return quota
}

func QuotaFromDecimalChecked(d decimal.Decimal) (int, *QuotaClamp) {
	f, _ := d.Round(0).Float64()
	return saturateQuota(f, "QuotaFromDecimal")
}
