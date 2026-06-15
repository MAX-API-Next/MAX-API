package doubao

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToGenericMediaRequestFromTopLevelFields(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 5,
		"end_image": "https://example.com/last-frame.png",
		"image": "https://example.com/first-frame.png",
		"input_mode": "single_image",
		"model": "doubao-seedance-2-0-260128",
		"prompt": "让人物自然转身并看向镜头",
		"resolution": "720p",
		"with_audio": true
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToGenericMediaRequest(&req)
	require.NoError(t, err)

	assert.Equal(t, "doubao-seedance-2-0-260128", payload.Model)
	assert.Equal(t, "让人物自然转身并看向镜头", payload.Prompt)
	assert.Equal(t, "16:9", payload.AspectRatio)
	assert.Equal(t, "video_generation", payload.Capability)
	assert.Equal(t, "end_frame", payload.ControlMode)
	assert.Equal(t, "single_image", payload.InputMode)
	assert.Equal(t, "https://example.com/first-frame.png", payload.Image)
	assert.Equal(t, "https://example.com/last-frame.png", payload.EndImage)
	assert.Equal(t, "720p", payload.Resolution)
	require.NotNil(t, payload.DurationSeconds)
	assert.Equal(t, 5, *payload.DurationSeconds)
	require.NotNil(t, payload.WithAudio)
	assert.True(t, *payload.WithAudio)
}

func TestConvertToGenericMediaRequestReferenceImages(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "参考图片中的人物在海边回眸",
		"reference_images": ["asset://asset-a", "asset://asset-b"],
		"with_audio": false
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToGenericMediaRequest(&req)
	require.NoError(t, err)

	assert.Equal(t, "multi_image", payload.InputMode)
	assert.Equal(t, "reference", payload.ControlMode)
	assert.Equal(t, []string{"asset://asset-a", "asset://asset-b"}, payload.ReferenceImages)
	require.NotNil(t, payload.WithAudio)
	assert.False(t, *payload.WithAudio)
}

func TestParseSeedanceMediaTaskResult(t *testing.T) {
	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"object": "media.task",
		"task_id": "6c1a4eeaaee14736a78170ddbfff8361",
		"status": "succeeded",
		"progress": 100,
		"result": {
			"url": "https://example.com/result.mp4",
			"duration_seconds": 5
		}
	}`))
	require.NoError(t, err)

	assert.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
	assert.Equal(t, "100%", taskInfo.Progress)
	assert.Equal(t, "https://example.com/result.mp4", taskInfo.Url)
	assert.Equal(t, "6c1a4eeaaee14736a78170ddbfff8361", taskInfo.TaskID)
}
