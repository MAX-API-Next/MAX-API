package taskcommon

import (
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/require"
)

func TestMergeTaskSubmitReqKeepsTopLevelSizeBeforeAspectRatio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"size":         "1280x720",
		"aspect_ratio": "9:16",
	})

	require.Equal(t, "1280x720", req.Size)
}

func TestMergeTaskSubmitReqUsesTopLevelAspectRatioWhenSizeMissing(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"aspect_ratio": "9:16",
	})

	require.Equal(t, "9:16", req.Size)
}

func TestMergeTaskSubmitReqKeepsTopLevelModelBeforeModelName(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"model":      "client-model",
		"model_name": "fallback-model",
	})

	require.Equal(t, "client-model", req.Model)
}

func TestMergeTaskSubmitReqUsesModelNameWhenModelMissing(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"model_name": "fallback-model",
	})

	require.Equal(t, "fallback-model", req.Model)
}

func TestMergeTaskSubmitReqKeepsSecondsBeforeDuration(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"seconds":  "8",
		"duration": float64(10),
	})

	require.Equal(t, "8", req.Seconds)
	require.NotNil(t, req.Duration)
	require.Equal(t, 10, *req.Duration)
}

func TestMergeTaskSubmitReqReflectsDurationIntoSecondsWhenSecondsMissing(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"duration": float64(5),
		"parameters": map[string]any{
			"duration":        float64(7),
			"durationSeconds": float64(10),
		},
	})

	require.Equal(t, "10", req.Seconds)
	require.NotNil(t, req.Duration)
	require.Equal(t, 10, *req.Duration)
}

func TestMergeTaskSubmitReqMapsParameterGenerateAudio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}

	mergeTaskSubmitReq(&req, map[string]any{
		"parameters": map[string]any{
			"audio":         false,
			"generateAudio": true,
		},
	})

	require.NotNil(t, req.WithAudio)
	require.False(t, *req.WithAudio)
	require.NotNil(t, req.GenerateAudio)
	require.True(t, *req.GenerateAudio)
}
