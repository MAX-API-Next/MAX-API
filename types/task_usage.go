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

func CloneTaskUsage(usage *TaskUsage) *TaskUsage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	cloned.OutputDurationMs = cloneInt64Pointer(usage.OutputDurationMs)
	cloned.InputVideoDurationMs = cloneInt64Pointer(usage.InputVideoDurationMs)
	cloned.InputAudioDurationMs = cloneInt64Pointer(usage.InputAudioDurationMs)
	cloned.InputImageCount = cloneInt64Pointer(usage.InputImageCount)
	cloned.InputVideoCount = cloneInt64Pointer(usage.InputVideoCount)
	cloned.InputAudioCount = cloneInt64Pointer(usage.InputAudioCount)
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
