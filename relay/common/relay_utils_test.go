package common

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateMultipartDirectNormalizesImageField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"wan2.7-i2v","prompt":"animate","image":" https://example.com/first.png "}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{
		TaskRelayInfo: &TaskRelayInfo{},
	}

	taskErr := ValidateMultipartDirect(context, info)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/first.png"}, storedReq.Images)
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestValidateMultipartDirectNormalizesInputReferenceField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"wan2.7-i2v","prompt":"animate","input_reference":" https://example.com/first.png "}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{
		TaskRelayInfo: &TaskRelayInfo{},
	}

	taskErr := ValidateMultipartDirect(context, info)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/first.png"}, storedReq.Images)
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestTaskDurationBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(body string) (*gin.Context, *RelayInfo) {
		request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = request
		return context, &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	}

	tests := []struct {
		name        string
		body        string
		wantErr     bool
		wantMessage string
	}{
		{
			name:        "huge duration is rejected",
			body:        `{"model":"sora-2","prompt":"a cat","duration":9999999999}`,
			wantErr:     true,
			wantMessage: "seconds must be between 0 and",
		},
		{
			name:        "huge seconds string is rejected",
			body:        `{"model":"sora-2","prompt":"a cat","seconds":"9999999999"}`,
			wantErr:     true,
			wantMessage: "seconds must be between 0 and",
		},
		{
			name:        "negative duration is rejected",
			body:        `{"model":"sora-2","prompt":"a cat","duration":-8}`,
			wantErr:     true,
			wantMessage: "seconds must be between 0 and",
		},
		{
			name:        "non numeric seconds string is rejected",
			body:        `{"model":"sora-2","prompt":"a cat","seconds":"abc"}`,
			wantErr:     true,
			wantMessage: "invalid seconds value: abc",
		},
		{
			name: "normal duration is accepted",
			body: `{"model":"sora-2","prompt":"a cat","seconds":"8"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" multipart direct", func(t *testing.T) {
			context, info := newContext(tt.body)
			taskErr := ValidateMultipartDirect(context, info)
			if tt.wantErr {
				require.NotNil(t, taskErr)
				require.Equal(t, "invalid_seconds", taskErr.Code)
				require.Contains(t, taskErr.Message, tt.wantMessage)
				return
			}
			require.Nil(t, taskErr)
		})

		t.Run(tt.name+" basic task request", func(t *testing.T) {
			context, info := newContext(tt.body)
			taskErr := ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)
			if tt.wantErr {
				require.NotNil(t, taskErr)
				require.Equal(t, "invalid_seconds", taskErr.Code)
				require.Contains(t, taskErr.Message, tt.wantMessage)
				return
			}
			require.Nil(t, taskErr)
		})
	}
}

func TestValidateMultipartDirectIgnoresBlankInputReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"wan2.7-i2v","prompt":"animate","input_reference":"   ","image":" https://example.com/first.png "}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{
		TaskRelayInfo: &TaskRelayInfo{},
	}

	taskErr := ValidateMultipartDirect(context, info)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/first.png"}, storedReq.Images)
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestValidateBasicTaskRequestNormalizesImageField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"wan2.7-i2v","prompt":"animate","image":" https://example.com/first.png "}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{
		TaskRelayInfo: &TaskRelayInfo{},
	}

	taskErr := ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/first.png"}, storedReq.Images)
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestValidateBasicTaskRequestScopesContentOnlyException(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		channelType   int
		upstreamModel string
		requestModel  string
		wantErr       bool
	}{
		{name: "H3 content", channelType: constant.ChannelTypeMiniMax, upstreamModel: "MiniMax-H3"},
		{name: "H3 request model fallback", channelType: constant.ChannelTypeMiniMax, requestModel: "MiniMax-H3"},
		{name: "Doubao content", channelType: constant.ChannelTypeDoubaoVideo},
		{name: "VolcEngine Doubao content", channelType: constant.ChannelTypeVolcEngine},
		{name: "legacy Hailuo requires prompt", channelType: constant.ChannelTypeMiniMax, upstreamModel: "MiniMax-Hailuo-2.3", wantErr: true},
		{name: "Gemini requires prompt", channelType: constant.ChannelTypeGemini, upstreamModel: "veo-3.0-generate-001", wantErr: true},
		{name: "Kling requires prompt", channelType: constant.ChannelTypeKling, upstreamModel: "kling-v2", wantErr: true},
		{name: "Jimeng requires prompt", channelType: constant.ChannelTypeJimeng, upstreamModel: "jimeng-video", wantErr: true},
		{name: "Vertex requires prompt", channelType: constant.ChannelTypeVertexAi, upstreamModel: "veo-3.0-generate-001", wantErr: true},
		{name: "Vidu requires prompt", channelType: constant.ChannelTypeVidu, upstreamModel: "viduq1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestModel := tt.requestModel
			if requestModel == "" {
				requestModel = "MiniMax-H3"
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
				"model":"`+requestModel+`",
				"content":[{"type":"text","text":"Create a short film"}]
			}`))
			request.Header.Set("Content-Type", "application/json")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = request
			info := &RelayInfo{
				TaskRelayInfo: &TaskRelayInfo{},
				ChannelMeta: &ChannelMeta{
					ChannelType:       tt.channelType,
					UpstreamModelName: tt.upstreamModel,
				},
			}

			taskErr := ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
			if tt.wantErr {
				require.NotNil(t, taskErr)
				require.Contains(t, taskErr.Message, "prompt is required")
				return
			}
			require.Nil(t, taskErr)
		})
	}
}
