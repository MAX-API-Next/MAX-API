package types

const (
	TaskUsageSourceRequestEstimate  = "request_estimate"
	TaskUsageSourceProviderResponse = "provider_response"
	TaskUsageSourceCallback         = "callback"
	TaskUsageSourceManualReconcile  = "manual_reconcile"
)

const (
	TaskUsageCompletenessComplete  = "complete"
	TaskUsageCompletenessPartial   = "partial"
	TaskUsageCompletenessMissing   = "missing"
	TaskUsageCompletenessInvalid   = "invalid"
	TaskUsageCompletenessAmbiguous = "ambiguous"
)

// TaskUsage is the provider-neutral usage fact used by asynchronous task
// billing. Pointer fields preserve the difference between an omitted field and
// an explicit zero returned by the provider.
type TaskUsage struct {
	OutputDurationMs     *int64 `json:"output_duration_ms,omitempty"`
	InputVideoDurationMs *int64 `json:"input_video_duration_ms,omitempty"`
	InputAudioDurationMs *int64 `json:"input_audio_duration_ms,omitempty"`
	InputImageCount      *int64 `json:"input_image_count,omitempty"`
	InputVideoCount      *int64 `json:"input_video_count,omitempty"`
	InputAudioCount      *int64 `json:"input_audio_count,omitempty"`
	Source               string `json:"source,omitempty"`
	Completeness         string `json:"completeness,omitempty"`
}
