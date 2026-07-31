package jimeng

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultKeepsErrorResponseAsFailure(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"code":50001,
		"message":"upstream failed",
		"data":{"status":"done","video_url":"https://example.com/video.mp4"}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusFailure), result.Status)
	require.Equal(t, "100%", result.Progress)
	require.Equal(t, "upstream failed", result.Reason)
}
