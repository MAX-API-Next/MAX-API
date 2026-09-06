package hailuo

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newH3TestInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: H3Model,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeMiniMax,
			ChannelBaseUrl:    H3TestBaseURL,
			UpstreamModelName: H3Model,
		},
	}
}

const H3TestBaseURL = "https://api.minimax.io"

func TestBuildH3RequestUsesOfficialV2ContentShape(t *testing.T) {
	duration := 5
	ratio := "16:9"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      H3Model,
		Prompt:     "A cinematic ocean sunrise",
		Duration:   &duration,
		Resolution: Resolution2K,
		Ratio:      &ratio,
		Content: []map[string]any{
			{"type": "text", "text": "A cinematic ocean sunrise"},
			{"type": "image_url", "role": "first_frame", "image_url": map[string]any{"url": "https://example.com/frame.png"}},
		},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, newH3TestInfo())
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var request H3VideoRequest
	require.NoError(t, common.Unmarshal(data, &request))
	require.Equal(t, H3Model, request.Model)
	require.Equal(t, Resolution2K, request.Resolution)
	require.Equal(t, 5, request.Duration)
	require.Equal(t, "adaptive", request.Ratio)
	require.Len(t, request.Content, 2)
	require.Equal(t, "https://example.com/frame.png", request.Content[1].ImageURL.URL)
}

func TestBuildH3RequestAndBillingPlanUseDurationSeconds(t *testing.T) {
	durationSeconds := 10
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:           H3Model,
		Prompt:          "A cinematic ocean sunrise",
		DurationSeconds: &durationSeconds,
		Content:         []map[string]any{{"type": "text", "text": "A cinematic ocean sunrise"}},
	})
	info := newH3TestInfo()
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var request H3VideoRequest
	require.NoError(t, common.Unmarshal(data, &request))
	require.Equal(t, 10, request.Duration)

	plan, err := (&TaskAdaptor{}).BuildTaskBillingPlan(c, info)
	require.NoError(t, err)
	require.EqualValues(t, 10, plan.RequestedOutputDurationSeconds)
}

func TestH3UsesEffectiveUpstreamModelAfterMapping(t *testing.T) {
	info := newH3TestInfo()
	info.OriginModelName = H3Model
	info.ChannelMeta.UpstreamModelName = "MiniMax-Hailuo-2.3"
	req := &relaycommon.TaskSubmitReq{Model: H3Model}

	require.False(t, h3RequestUsesModel(req, info))
	require.Equal(t, "MiniMax-Hailuo-2.3", h3RequestModel(req, info))

	info.ChannelMeta.UpstreamModelName = H3Model
	info.OriginModelName = "h3-alias"
	require.True(t, h3RequestUsesModel(req, info))
	require.Equal(t, H3Model, h3RequestModel(req, info))
}

func TestValidateH3RequestAcceptsTextOnlyInsideContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"MiniMax-H3",
		"content":[{"type":"text","text":"Create a short film"}]
	}`))
	request.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = request

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newH3TestInfo())
	require.Nil(t, taskErr)
}

func TestValidateH3RequestRejectsContentOnlyAfterMappingToLegacyModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"MiniMax-H3",
		"content":[{"type":"text","text":"Create a short film"}]
	}`))
	request.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = request
	c.Set("model_mapping", `{"MiniMax-H3":"MiniMax-Hailuo-2.3"}`)
	info := newH3TestInfo()

	require.NoError(t, helper.ModelMappedHelper(c, info, nil))
	require.Equal(t, "MiniMax-Hailuo-2.3", info.UpstreamModelName)
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.Contains(t, taskErr.Message, "prompt is required")
}

func TestBuildH3RequestMapsReferenceVideoAndAudio(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  H3Model,
		Prompt: "Match the reference performance",
		Metadata: map[string]any{
			"reference_video": []any{"https://example.com/reference.mp4"},
			"reference_audio": []any{"https://example.com/reference.mp3"},
		},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, newH3TestInfo())
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var request H3VideoRequest
	require.NoError(t, common.Unmarshal(data, &request))
	require.Equal(t, "adaptive", request.Ratio)
	require.Len(t, request.Content, 3)
	require.Equal(t, "reference_video", request.Content[1].Role)
	require.Equal(t, "reference_audio", request.Content[2].Role)
}

func TestBuildH3RequestNormalizesCallbackURL(t *testing.T) {
	direct := "  https://example.com/direct-callback  "
	directBlank := "   "
	tests := []struct {
		name        string
		callbackURL *string
		metadata    map[string]any
		wantURL     string
		wantNil     bool
		wantErr     string
	}{
		{
			name:        "direct value is trimmed",
			callbackURL: &direct,
			metadata:    map[string]any{"callback_url": 42},
			wantURL:     "https://example.com/direct-callback",
		},
		{
			name:        "direct blank remains authoritative and is omitted",
			callbackURL: &directBlank,
			metadata:    map[string]any{"callback_url": "https://example.com/metadata-callback"},
			wantNil:     true,
		},
		{
			name:     "metadata value is trimmed",
			metadata: map[string]any{"callback_url": "  https://example.com/metadata-callback  "},
			wantURL:  "https://example.com/metadata-callback",
		},
		{
			name:     "metadata blank is omitted",
			metadata: map[string]any{"callback_url": "  "},
			wantNil:  true,
		},
		{
			name:     "metadata null is omitted",
			metadata: map[string]any{"callback_url": nil},
			wantNil:  true,
		},
		{
			name:     "metadata non-string is rejected",
			metadata: map[string]any{"callback_url": 42},
			wantErr:  "callback_url must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := buildH3Request(&relaycommon.TaskSubmitReq{
				Model:       H3Model,
				Prompt:      "Create a video",
				CallbackURL: tt.callbackURL,
				Metadata:    tt.metadata,
			}, newH3TestInfo())
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				require.Nil(t, request.CallbackURL)
				return
			}
			require.NotNil(t, request.CallbackURL)
			require.Equal(t, tt.wantURL, *request.CallbackURL)
		})
	}
}

func TestBuildH3RequestRejectsNonStringMetadataOptions(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		wantErr  string
	}{
		{
			name:     "resolution",
			metadata: map[string]any{"resolution": 1080},
			wantErr:  "resolution must be a string",
		},
		{
			name:     "ratio",
			metadata: map[string]any{"ratio": 16},
			wantErr:  "ratio must be a string",
		},
		{
			name:     "resolution null",
			metadata: map[string]any{"resolution": nil},
			wantErr:  "resolution must be a string",
		},
		{
			name:     "ratio null",
			metadata: map[string]any{"ratio": nil},
			wantErr:  "ratio must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildH3Request(&relaycommon.TaskSubmitReq{
				Model:    H3Model,
				Prompt:   "Create a video",
				Metadata: tt.metadata,
			}, newH3TestInfo())
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestBuildH3BillingPlanCapturesRequestFactsWithoutChargingAudio(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  H3Model,
		Prompt: "Match the references",
		Content: []map[string]any{
			{"type": "text", "text": "Match the references"},
			{"type": "image_url", "role": "reference_image", "image_url": map[string]any{"url": "https://example.com/1.png"}},
			{"type": "video_url", "role": "reference_video", "video_url": map[string]any{"url": "https://example.com/1.mp4"}},
			{"type": "video_url", "role": "reference_video", "video_url": map[string]any{"url": "https://example.com/2.mp4"}},
			{"type": "audio_url", "role": "reference_audio", "audio_url": map[string]any{"url": "https://example.com/1.mp3"}},
		},
	})
	info := newH3TestInfo()
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	plan, err := (&TaskAdaptor{}).BuildTaskBillingPlan(c, info)
	require.NoError(t, err)
	require.EqualValues(t, 2, plan.InputVideoCount)
	require.EqualValues(t, 1, plan.InputAudioCount)
	require.EqualValues(t, 1, plan.InputImageCount)
	require.NotEmpty(t, plan.ConfigHash)
	// The two videos share one aggregate 15-second reserve; audio is free.
	require.EqualValues(t, 1600, plan.ReserveQuota)
}

func TestH3BuildRequestRejectsUnsupportedResolution(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      H3Model,
		Prompt:     "Create a video",
		Resolution: "1080P",
	})

	_, err := (&TaskAdaptor{}).BuildRequestBody(c, newH3TestInfo())
	require.Error(t, err)
	require.Contains(t, err.Error(), "768P or 2K")
}

func TestH3MaxIsRejectedInsteadOfUsingLegacyV1Protocol(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  H3MaxModel,
		Prompt: "Create a video",
	})
	info := newH3TestInfo()
	info.OriginModelName = H3MaxModel
	info.ChannelMeta.UpstreamModelName = H3MaxModel

	_, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.Error(t, err)
	require.Contains(t, err.Error(), H3MaxModel)

	_, err = (&TaskAdaptor{baseURL: H3TestBaseURL}).BuildRequestURL(info)
	require.Error(t, err)
	require.Contains(t, err.Error(), H3MaxModel)
}

func TestH3QueryUsesV2PathAndEscapesTaskID(t *testing.T) {
	service.InitHttpClient()
	requestPath := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath <- r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "test-key", map[string]any{
		"model":   H3Model,
		"task_id": "task/with-special-id",
	}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	select {
	case path := <-requestPath:
		require.Equal(t, "/v2/query/video_generation/task%2Fwith-special-id", path)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the H3 query request")
	}
}

func TestH3DoResponseAcceptsBareTaskID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := newH3TestInfo()
	info.PublicTaskID = "task_public"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"task_id":"upstream-task"}`)),
	}

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "upstream-task", taskID)
	require.JSONEq(t, `{"task_id":"upstream-task"}`, string(taskData))
	require.Equal(t, http.StatusOK, recorder.Code)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &video))
	require.Equal(t, "task_public", video.ID)
	require.Equal(t, dto.VideoStatusQueued, video.Status)
}

func TestH3DoResponseRejectsProviderError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := newH3TestInfo()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"invalid content","http_code":400}}`)),
	}

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Empty(t, taskID)
	require.Empty(t, taskData)
	require.NotNil(t, taskErr)
	require.Equal(t, "400", taskErr.Code)
	require.Contains(t, taskErr.Error.Error(), "invalid content")
}

func TestH3DoResponseRejectsBaseRespError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := newH3TestInfo()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"base_resp":{"status_code":1004,"status_msg":"invalid request"}}`)),
	}

	_, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.NotNil(t, taskErr)
	require.Equal(t, "1004", taskErr.Code)
	require.Contains(t, taskErr.Error.Error(), "invalid request")
}

func TestParseH3TaskResultCarriesVideoAndAudioUsageSeparately(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"task": {
			"id": "task-1",
			"model": "MiniMax-H3",
			"status": "succeeded",
			"content": {"url": "https://cdn.example.com/result.mp4"},
			"usage": {
				"total_seconds": 5,
				"input_seconds": 0,
				"output_seconds": 5,
				"input_image_count": 1,
				"input_audio_seconds": 6
			}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "https://cdn.example.com/result.mp4", result.Url)
	require.NotNil(t, result.Usage)
	require.Equal(t, types.TaskUsageCompletenessComplete, result.Usage.Completeness)
	require.Equal(t, int64(5000), *result.Usage.OutputDurationMs)
	require.Equal(t, int64(0), *result.Usage.InputVideoDurationMs)
	require.Equal(t, int64(6000), *result.Usage.InputAudioDurationMs)
	require.Equal(t, int64(1), *result.Usage.InputImageCount)
	require.Equal(t, types.TaskUsageSourceProviderResponse, result.Usage.Source)
}

func TestParseH3UsagePreservesExplicitZeroAndMissingFields(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"task": {
			"id": "task-2",
			"status": "succeeded",
			"content": {"url": "https://cdn.example.com/result.mp4"},
			"usage": {
				"total_seconds": 5,
				"input_seconds": 0,
				"output_seconds": 5,
				"input_image_count": 0
			}
		}
	}`))
	require.NoError(t, err)
	require.NotNil(t, result.Usage.InputVideoDurationMs)
	require.Equal(t, int64(0), *result.Usage.InputVideoDurationMs)
	require.Nil(t, result.Usage.InputAudioDurationMs)

	missing, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"task": {"id":"task-3","status":"succeeded","content":{"url":"https://cdn.example.com/result.mp4"}}
	}`))
	require.NoError(t, err)
	require.Equal(t, types.TaskUsageCompletenessMissing, missing.Usage.Completeness)
}

func TestParseH3UsageMarksInvalidProviderValues(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"task": {
			"id": "task-4",
			"status": "succeeded",
			"content": {"url": "https://cdn.example.com/result.mp4"},
			"usage": {
				"total_seconds": 5,
				"input_seconds": 0,
				"output_seconds": "not-a-number",
				"input_image_count": "not-a-number"
			}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, types.TaskUsageCompletenessInvalid, result.Usage.Completeness)
	require.Nil(t, result.Usage.InputImageCount)
}

func TestParseH3UsagePreservesFractionalVideoAndAudioSeconds(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"task": {
			"id": "task-fractional",
			"status": "succeeded",
			"content": {"url": "https://cdn.example.com/result.mp4"},
			"usage": {
				"total_seconds": 12.5,
				"input_seconds": 7.5,
				"output_seconds": 5,
				"input_image_count": 1,
				"input_audio_seconds": 2.25
			}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, types.TaskUsageCompletenessComplete, result.Usage.Completeness)
	require.Equal(t, int64(7_500), *result.Usage.InputVideoDurationMs)
	require.Equal(t, int64(2_250), *result.Usage.InputAudioDurationMs)
	require.Equal(t, int64(5_000), *result.Usage.OutputDurationMs)
}

func TestConvertH3TaskToOpenAIVideo(t *testing.T) {
	tests := []struct {
		name       string
		status     model.TaskStatus
		data       string
		wantStatus string
		wantURL    string
	}{
		{
			name:       "success",
			status:     model.TaskStatusSuccess,
			data:       `{"task":{"id":"upstream-task","model":"MiniMax-H3","status":"succeeded","content":{"url":"https://cdn.example.com/video.mp4"}}}`,
			wantStatus: dto.VideoStatusCompleted,
			wantURL:    "https://cdn.example.com/video.mp4",
		},
		{
			name:       "success without content",
			status:     model.TaskStatusSuccess,
			data:       `{"task":{"id":"upstream-task","model":"MiniMax-H3","status":"succeeded"}}`,
			wantStatus: dto.VideoStatusInProgress,
		},
		{
			name:       "success with blank content url",
			status:     model.TaskStatusSuccess,
			data:       `{"task":{"id":"upstream-task","model":"MiniMax-H3","status":"succeeded","content":{"url":"  "}}}`,
			wantStatus: dto.VideoStatusInProgress,
		},
		{
			name:       "failure",
			status:     model.TaskStatusFailure,
			data:       `{"task":{"id":"upstream-task","model":"MiniMax-H3","status":"failed","error":{"code":3001,"message":"generation failed"}}}`,
			wantStatus: dto.VideoStatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(&model.Task{
				TaskID:     "task_public",
				Status:     tt.status,
				Properties: model.Properties{OriginModelName: H3Model},
				Data:       []byte(tt.data),
			})
			require.NoError(t, err)

			var video dto.OpenAIVideo
			require.NoError(t, common.Unmarshal(body, &video))
			require.Equal(t, tt.wantStatus, video.Status)
			if tt.wantURL != "" {
				require.Equal(t, tt.wantURL, video.Metadata["url"])
			}
			if tt.name == "failure" {
				require.NotNil(t, video.Error)
				require.Equal(t, "generation failed", video.Error.Message)
				require.Equal(t, "3001", video.Error.Code)
			}
		})
	}
}

func TestH3TransientQueryErrorKeepsPollingRetryable(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"type":"error",
		"error":{"type":"rate_limit_error","message":"retry later","http_code":"429"}
	}`))
	require.Error(t, err)
	require.Nil(t, result)
}

func TestParseTaskResultWaitsForRetrievableVideoURL(t *testing.T) {
	service.InitHttpClient()

	requestPath := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath <- r.URL.Path
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	adaptor := &TaskAdaptor{apiKey: "test-key", baseURL: server.URL}
	result, err := adaptor.ParseTaskResult([]byte(`{
		"task_id":"task-1",
		"status":"Success",
		"file_id":"file-1",
		"base_resp":{"status_code":0}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusInProgress), result.Status)
	require.Equal(t, "90%", result.Progress)
	require.Empty(t, result.Url)

	select {
	case path := <-requestPath:
		require.Equal(t, "/v1/files/retrieve", path)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the Hailuo file retrieval request")
	}
}
