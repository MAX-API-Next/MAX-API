package doubao

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestProduceUsagePreservesDoubaoExplicitZero(t *testing.T) {
	envelope, err := (&TaskAdaptor{}).ProduceUsage(types.TaskUsageContext{
		Stage:   types.TaskUsageSourceProviderResponse,
		Payload: []byte(`{"status":"succeeded","usage":{"completion_tokens":0,"total_tokens":0}}`),
	})
	require.NoError(t, err)
	require.Equal(t, types.TaskUsageProducerKindGoAdapter, envelope.ProducerKind)
	require.Equal(t, taskusage.DoubaoVideoContract().SourceID, envelope.SourceID)
	require.Equal(t, types.TaskUsagePresencePresentZero, envelope.Presence)
	require.NotNil(t, envelope.Usage.CompletionTokens)
	require.NotNil(t, envelope.Usage.TotalTokens)
	require.Zero(t, *envelope.Usage.CompletionTokens)
	require.Zero(t, *envelope.Usage.TotalTokens)
}

func TestProduceUsageClassifiesDoubaoMissingPartialAndInvalid(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		presence     string
		completeness string
	}{
		{name: "missing", body: `{"status":"succeeded"}`, presence: types.TaskUsagePresenceNotPresent, completeness: types.TaskUsageCompletenessMissing},
		{name: "partial", body: `{"status":"succeeded","usage":{"completion_tokens":3}}`, presence: types.TaskUsagePresencePartial, completeness: types.TaskUsageCompletenessPartial},
		{name: "single negative completion", body: `{"status":"succeeded","usage":{"completion_tokens":-1}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
		{name: "single negative total", body: `{"status":"succeeded","usage":{"total_tokens":-1}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
		{name: "negative", body: `{"status":"succeeded","usage":{"completion_tokens":-1,"total_tokens":2}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
		{name: "inconsistent", body: `{"status":"succeeded","usage":{"completion_tokens":4,"total_tokens":3}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
		{name: "non numeric token", body: `{"status":"succeeded","usage":{"completion_tokens":"many"}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
		{name: "null usage", body: `{"status":"succeeded","usage":null}`, presence: types.TaskUsagePresenceNotPresent, completeness: types.TaskUsageCompletenessMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope, err := (&TaskAdaptor{}).ProduceUsage(types.TaskUsageContext{
				Stage: types.TaskUsageSourceProviderResponse, Payload: []byte(test.body),
			})
			require.NoError(t, err)
			require.Equal(t, test.presence, envelope.Presence)
			require.Equal(t, test.completeness, envelope.Completeness)
		})
	}
}

func TestProduceUsageConfiguredResponseIgnoresIncompatibleUnrelatedFields(t *testing.T) {
	envelope, err := (&TaskAdaptor{}).ProduceUsage(types.TaskUsageContext{
		Stage: types.TaskUsageSourceProviderResponse,
		Payload: []byte(`{
			"code":"success",
			"data":{"data":{
				"id":"task_wrapped",
				"status":"succeeded",
				"duration":"5",
				"created_at":"not-a-timestamp",
				"updated_at":false,
				"usage":{"completion_tokens":12,"total_tokens":20}
			}}
		}`),
	})
	require.NoError(t, err)
	require.Equal(t, types.TaskUsagePresencePresentValid, envelope.Presence)
	require.EqualValues(t, 12, *envelope.Usage.CompletionTokens)
	require.EqualValues(t, 20, *envelope.Usage.TotalTokens)
}

func TestParseTaskResultAttachesDoubaoUsageEnvelopeWithoutChangingLegacyTokens(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status":"succeeded",
		"content":{"video_url":"https://cdn.example.com/video.mp4"},
		"usage":{"completion_tokens":12,"total_tokens":20}
	}`))
	require.NoError(t, err)
	require.Equal(t, 12, result.CompletionTokens)
	require.Equal(t, 20, result.TotalTokens)
	require.NotNil(t, result.UsageEnvelope)
	require.Equal(t, types.TaskUsagePresencePresentValid, result.UsageEnvelope.Presence)
	require.NotNil(t, result.UsageEnvelope.Usage)
	require.NotNil(t, result.UsageEnvelope.Usage.CompletionTokens)
	require.EqualValues(t, result.CompletionTokens, *result.UsageEnvelope.Usage.CompletionTokens)
	require.NotNil(t, result.UsageEnvelope.Usage.TotalTokens)
	require.EqualValues(t, result.TotalTokens, *result.UsageEnvelope.Usage.TotalTokens)
}

func TestParseTaskResultDoesNotLetNonObjectUsageHideTerminalResult(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status":"succeeded",
		"content":{"video_url":"https://cdn.example.com/video.mp4"},
		"usage":[]
	}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "https://cdn.example.com/video.mp4", result.Url)
	require.NotNil(t, result.UsageEnvelope)
	require.Equal(t, types.TaskUsagePresenceInvalid, result.UsageEnvelope.Presence)
	require.Equal(t, types.TaskUsageCompletenessInvalid, result.UsageEnvelope.Completeness)
}

func TestParseTaskResultDoesNotCopyInvalidUsageIntoLegacyBillingFields(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status":"succeeded",
		"content":{"video_url":"https://cdn.example.com/video.mp4"},
		"usage":{"completion_tokens":100,"total_tokens":1}
	}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, types.TaskUsageCompletenessInvalid, result.UsageEnvelope.Completeness)
	// Invalid totals must not reach the compatibility fields used by legacy
	// token-ratio settlement.
	require.Zero(t, result.CompletionTokens)
	require.Zero(t, result.TotalTokens)
}

func TestProduceUsageUnwrapsConfiguredRelayEnvelope(t *testing.T) {
	envelope, err := (&TaskAdaptor{}).ProduceUsage(types.TaskUsageContext{
		Stage: types.TaskUsageSourceProviderResponse,
		Payload: []byte(`{
			"code":"success",
			"data":{"data":{
				"id":"task_123",
				"status":"succeeded",
				"usage":{"completion_tokens":12,"total_tokens":20}
			}}
		}`),
	})
	require.NoError(t, err)
	require.Equal(t, types.TaskUsagePresencePresentValid, envelope.Presence)
	require.EqualValues(t, 12, *envelope.Usage.CompletionTokens)
	require.EqualValues(t, 20, *envelope.Usage.TotalTokens)
}

func TestParseTaskResultUsesTheSameWrappedUsageAsProduceUsage(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"code":"success",
		"data":{"data":{
			"id":"task_wrapped",
			"status":"succeeded",
			"content":{"video_url":"https://cdn.example.com/wrapped.mp4"},
			"usage":{"completion_tokens":12,"total_tokens":20}
		}}
	}`))
	require.NoError(t, err)
	require.NotNil(t, result.UsageEnvelope)
	require.Equal(t, types.TaskUsagePresencePresentValid, result.UsageEnvelope.Presence)
	require.EqualValues(t, 12, *result.UsageEnvelope.Usage.CompletionTokens)
	require.EqualValues(t, 20, *result.UsageEnvelope.Usage.TotalTokens)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "https://cdn.example.com/wrapped.mp4", result.Url)
}

func TestParseTaskResultUnwrapsNestedProviderPayloadForStatusAndURL(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"code":"success",
		"data":{"data":{
			"id":"task_wrapped",
			"status":"succeeded",
			"content":{"video_url":"https://cdn.example.com/nested.mp4"},
			"usage":{"completion_tokens":12,"total_tokens":20}
		}}
	}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "https://cdn.example.com/nested.mp4", result.Url)
	require.EqualValues(t, 12, result.CompletionTokens)
	require.EqualValues(t, 20, result.TotalTokens)
}
