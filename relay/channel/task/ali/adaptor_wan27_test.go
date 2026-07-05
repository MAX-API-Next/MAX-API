package ali

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
}

func TestConvertToAliRequestWan27I2VBuildsMediaFromImage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	duration := 10
	req := relaycommon.TaskSubmitReq{
		Model:    "wan2.7-i2v",
		Prompt:   "animate the first frame",
		Image:    "https://example.com/first.png",
		Size:     "720p",
		Duration: &duration,
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, "wan2.7-i2v", aliReq.Model)
	assert.Equal(t, "720P", aliReq.Parameters.Resolution)
	assert.Equal(t, 10, aliReq.Parameters.Duration)
	assert.Equal(t, []map[string]interface{}{
		{"type": "first_frame", "url": "https://example.com/first.png"},
	}, aliReq.Input.Media)
	assert.Empty(t, aliReq.Input.ImgURL)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"media"`)
	assert.NotContains(t, string(body), `"img_url"`)
}

func TestConvertToAliRequestWan27I2VPrefersInputReferenceOverImage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:          "wan2.7-i2v",
		Prompt:         "animate the validated input reference",
		Image:          "https://example.com/direct-image.png",
		InputReference: "https://example.com/input-reference.png",
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, []map[string]interface{}{
		{"type": "first_frame", "url": "https://example.com/input-reference.png"},
	}, aliReq.Input.Media)
	assert.Empty(t, aliReq.Input.ImgURL)
}

func TestConvertToAliRequestWan27I2VBuildsFirstAndLastFrameFromImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "interpolate between frames",
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/last.png",
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, []map[string]interface{}{
		{"type": "first_frame", "url": "https://example.com/first.png"},
		{"type": "last_frame", "url": "https://example.com/last.png"},
	}, aliReq.Input.Media)
}

func TestConvertToAliRequestWan27I2VUsesImagesAsLastFrameWhenInputReferenceIsFirstFrame(t *testing.T) {
	adaptor := &TaskAdaptor{}

	tests := []struct {
		name  string
		input []string
		want  []map[string]interface{}
	}{
		{
			name:  "single image becomes last frame",
			input: []string{"https://example.com/last.png"},
			want: []map[string]interface{}{
				{"type": "first_frame", "url": "https://example.com/input-reference.png"},
				{"type": "last_frame", "url": "https://example.com/last.png"},
			},
		},
		{
			name: "first non-empty image becomes last frame",
			input: []string{
				" ",
				"https://example.com/last.png",
				"https://example.com/ignored.png",
			},
			want: []map[string]interface{}{
				{"type": "first_frame", "url": "https://example.com/input-reference.png"},
				{"type": "last_frame", "url": "https://example.com/last.png"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := relaycommon.TaskSubmitReq{
				Model:          "wan2.7-i2v",
				Prompt:         "interpolate from input reference",
				InputReference: "https://example.com/input-reference.png",
				Images:         tt.input,
			}

			aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

			require.NoError(t, err)
			assert.Equal(t, tt.want, aliReq.Input.Media)
		})
	}
}

func TestConvertToAliRequestWan27I2VKeepsExplicitMetadataMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:          "wan2.7-i2v",
		Prompt:         "continue the clip",
		Image:          "https://example.com/direct.png",
		InputReference: "https://example.com/input-reference.png",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{
						"type": "first_clip",
						"url":  "https://example.com/input.mp4",
					},
				},
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, []map[string]interface{}{
		{"type": "first_clip", "url": "https://example.com/input.mp4"},
	}, aliReq.Input.Media)
	assert.Empty(t, aliReq.Input.ImgURL)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"media"`)
	assert.NotContains(t, string(body), `"img_url"`)
}

func TestConvertToAliRequestWan27I2VMergesLegacyMediaFieldsIntoPartialMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "continue the clip with ending frame and audio",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{
						"type": "first_clip",
						"url":  "https://example.com/input.mp4",
					},
				},
				"last_frame_url": "https://example.com/last.png",
				"audio_url":      "https://example.com/audio.mp3",
			},
		},
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, []map[string]interface{}{
		{"type": "first_clip", "url": "https://example.com/input.mp4"},
		{"type": "last_frame", "url": "https://example.com/last.png"},
		{"type": "driving_audio", "url": "https://example.com/audio.mp3"},
	}, aliReq.Input.Media)
	assert.Empty(t, aliReq.Input.LastFrameURL)
	assert.Empty(t, aliReq.Input.AudioURL)
}

func TestConvertToAliRequestWan27I2VRejectsMediaWithoutPrimaryInput(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "continue without a primary frame or clip",
		Metadata: map[string]interface{}{
			"input": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{
						"type": "last_frame",
						"url":  "https://example.com/last.png",
					},
					map[string]interface{}{
						"type": "driving_audio",
						"url":  "https://example.com/audio.mp3",
					},
				},
			},
		},
	}

	_, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "first_frame or first_clip")
}

func TestConvertToAliRequestWan27I2VRequiresMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.7-i2v",
		Prompt: "animate without a frame",
	}

	_, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "requires image"))
}

func TestConvertToAliRequestWan25I2VKeepsLegacyImgURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "wan2.5-i2v-preview",
		Prompt: "animate the first frame",
		Image:  "https://example.com/first.png",
	}

	aliReq, err := adaptor.convertToAliRequest(testRelayInfo(), req)

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/first.png", aliReq.Input.ImgURL)
	assert.Empty(t, aliReq.Input.Media)

	body, err := common.Marshal(aliReq)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"img_url"`)
	assert.NotContains(t, string(body), `"media"`)
}
