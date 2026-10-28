package dto

import "time"

const (
	ChannelRoutingHealthNormal          = "normal"
	ChannelRoutingHealthPenalized       = "penalized"
	ChannelRoutingHealthRuntimeDisabled = "runtime_disabled"
	ChannelRoutingHealthPolicyOff       = "policy_off"
	ChannelRoutingHealthUnknown         = "unknown"

	ChannelRoutingHealthSourceRedis       = "redis"
	ChannelRoutingHealthSourcePolicyOff   = "policy_off"
	ChannelRoutingHealthSourceUnavailable = "unavailable"
)

// ChannelRoutingHealth is a read-only management snapshot, not channel configuration.
// Nullable fields distinguish an unavailable observation from a known zero or false.
type ChannelRoutingHealth struct {
	SchemaVersion         int        `json:"schema_version"`
	Status                string     `json:"status"`
	Source                string     `json:"source"`
	BasePriority          int64      `json:"base_priority"`
	EffectivePriority     *int64     `json:"effective_priority"`
	StoredPenalty         *int64     `json:"stored_penalty"`
	ActivePenalty         *int64     `json:"active_penalty"`
	StoredRuntimeDisabled *bool      `json:"stored_runtime_disabled"`
	RuntimeDisabled       *bool      `json:"runtime_disabled"`
	PenaltyUntil          *time.Time `json:"penalty_until"`
	DisabledUntil         *time.Time `json:"disabled_until"`
	PenaltyRecoveryMode   *string    `json:"penalty_recovery_mode"`
	DisableRecoveryMode   *string    `json:"disable_recovery_mode"`
	LastObservedAt        *time.Time `json:"last_observed_at"`
	EvaluatedAt           time.Time  `json:"evaluated_at"`
	UnavailableReason     *string    `json:"unavailable_reason"`
}
