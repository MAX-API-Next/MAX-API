package types

const (
	TaskBillingPlanModeBoundedActual = "bounded_actual"
	TaskBillingPlanStageEstimate     = "estimate"
	TaskBillingPlanStageReserve      = "reserve"
	TaskBillingPlanStageFinal        = "final"
)

// TaskBillingPlan is an immutable, provider-neutral snapshot of the pricing
// facts selected for one asynchronous task. It is intentionally limited to
// registered components; it is not an executable expression or plugin.
type TaskBillingPlan struct {
	Source        string  `json:"source"`
	RuleKey       string  `json:"rule_key"`
	SchemaVersion int     `json:"schema_version"`
	Mode          string  `json:"mode"`
	ConfigHash    string  `json:"config_hash"`
	Currency      string  `json:"currency"`
	GroupRatio    float64 `json:"group_ratio"`
	QuotaPerUnit  float64 `json:"quota_per_unit"`
	Resolution    string  `json:"resolution"`

	RequestedOutputDurationSeconds int64 `json:"requested_output_duration_seconds"`
	InputVideoCount                int64 `json:"input_video_count"`
	InputAudioCount                int64 `json:"input_audio_count"`
	InputImageCount                int64 `json:"input_image_count"`

	Components    []TaskBillingPlanComponent `json:"components"`
	EstimateQuota int                        `json:"estimate_quota"`
	ReserveQuota  int                        `json:"reserve_quota"`
}

type TaskBillingPlanComponent struct {
	Key               string `json:"key"`
	Unit              string `json:"unit"`
	UnitPrice         string `json:"unit_price"`
	RequestedQuantity int64  `json:"requested_quantity"`
	ReservedQuantity  int64  `json:"reserved_quantity"`
	FreeQuantity      int64  `json:"free_quantity"`
}

func CloneTaskBillingPlan(plan *TaskBillingPlan) *TaskBillingPlan {
	if plan == nil {
		return nil
	}
	cloned := *plan
	cloned.Components = append([]TaskBillingPlanComponent(nil), plan.Components...)
	return &cloned
}
