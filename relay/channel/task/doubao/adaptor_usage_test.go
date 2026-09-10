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
		{name: "negative", body: `{"status":"succeeded","usage":{"completion_tokens":-1,"total_tokens":2}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
		{name: "inconsistent", body: `{"status":"succeeded","usage":{"completion_tokens":4,"total_tokens":3}}`, presence: types.TaskUsagePresenceInvalid, completeness: types.TaskUsageCompletenessInvalid},
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
