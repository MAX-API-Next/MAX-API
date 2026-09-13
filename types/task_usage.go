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

const (
	TaskUsageProducerKindGoAdapter  = "go_adapter"
	TaskUsageProducerKindTaskPlugin = "task_plugin"
)

const (
	TaskUsagePresenceNotPresent   = "not_present"
	TaskUsagePresencePresentZero  = "present_zero"
	TaskUsagePresencePresentValid = "present_valid"
	TaskUsagePresencePartial      = "partial"
	TaskUsagePresenceInvalid      = "invalid"
	TaskUsagePresenceAmbiguous    = "ambiguous"
)

const (
	TaskUsageFieldOutputDurationMs     = "output_duration_ms"
	TaskUsageFieldInputVideoDurationMs = "input_video_duration_ms"
	TaskUsageFieldInputAudioDurationMs = "input_audio_duration_ms"
	TaskUsageFieldInputImageCount      = "input_image_count"
	TaskUsageFieldInputVideoCount      = "input_video_count"
	TaskUsageFieldInputAudioCount      = "input_audio_count"
	TaskUsageFieldCompletionTokens     = "completion_tokens"
	TaskUsageFieldTotalTokens          = "total_tokens"
)

const (
	TaskUsageUnitMillisecond = "millisecond"
	TaskUsageUnitCount       = "count"
	TaskUsageUnitToken       = "token"
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
	CompletionTokens     *int64 `json:"completion_tokens,omitempty"`
	TotalTokens          *int64 `json:"total_tokens,omitempty"`
	Source               string `json:"source,omitempty"`
	Completeness         string `json:"completeness,omitempty"`
}

// TaskUsageFieldContract declares one bounded canonical fact. It is data-only:
// it cannot parse provider payloads, select prices, or mutate task/accounting
// state.
type TaskUsageFieldContract struct {
	Key                 string `json:"key"`
	Unit                string `json:"unit"`
	RequiredForTerminal bool   `json:"required_for_terminal,omitempty"`
	MinValue            *int64 `json:"min_value,omitempty"`
	MaxValue            *int64 `json:"max_value,omitempty"`
}

// TaskUsageContract identifies the finite canonical facts produced for one
// provider/source schema. SourceID and SchemaVersion are host-owned identity,
// never values accepted from a client request.
type TaskUsageContract struct {
	SourceID      string                   `json:"source_id"`
	SchemaVersion int                      `json:"schema_version"`
	Fields        []TaskUsageFieldContract `json:"fields"`
}

// TaskUsageContext is deliberately restricted to the fact production stage
// and payload. It carries no URL, credential, price, task mutation, or funding
// authority.
type TaskUsageContext struct {
	Stage   string
	Payload []byte
}

// TaskUsageEnvelope binds canonical facts to their producer and versioned
// contract. Digests cover only canonical contract/fact material and never the
// raw provider payload.
type TaskUsageEnvelope struct {
	ProducerKind   string     `json:"producer_kind"`
	SourceID       string     `json:"source_id"`
	SchemaVersion  int        `json:"schema_version"`
	ContractDigest string     `json:"contract_digest"`
	Stage          string     `json:"stage"`
	Presence       string     `json:"presence"`
	Completeness   string     `json:"completeness"`
	EvidenceDigest string     `json:"evidence_digest"`
	Usage          *TaskUsage `json:"usage,omitempty"`
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
	cloned.CompletionTokens = cloneInt64Pointer(usage.CompletionTokens)
	cloned.TotalTokens = cloneInt64Pointer(usage.TotalTokens)
	return &cloned
}

func CloneTaskUsageEnvelope(envelope *TaskUsageEnvelope) *TaskUsageEnvelope {
	if envelope == nil {
		return nil
	}
	cloned := *envelope
	cloned.Usage = CloneTaskUsage(envelope.Usage)
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
