package ali

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
)

func TestConvertToAliKlingRequestMapsKlingOfficialImageList(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "kling/kling-v3-omni-video-generation",
		Prompt: "让<<<image_1>>>中的人物向镜头挥手",
		Metadata: map[string]interface{}{
			"image_list": []interface{}{
				map[string]interface{}{"image_url": "https://example.com/image.png"},
			},
			"duration":     "5",
			"mode":         "pro",
			"sound":        "on",
			"aspect_ratio": "16:9",
		},
	}

	got, err := adaptor.convertToAliKlingRequest(req.Model, req)
	if err != nil {
		t.Fatalf("convertToAliKlingRequest returned error: %v", err)
	}
	if got.Model != req.Model {
		t.Fatalf("model = %q, want %q", got.Model, req.Model)
	}
	if got.Parameters == nil || got.Parameters.Duration != 5 || got.Parameters.Mode != "pro" || got.Parameters.AspectRatio != "16:9" {
		t.Fatalf("unexpected parameters: %+v", got.Parameters)
	}
	if got.Parameters.Audio == nil || *got.Parameters.Audio != true {
		t.Fatalf("audio = %v, want true", got.Parameters.Audio)
	}
	if len(got.Input.Media) != 1 {
		t.Fatalf("media length = %d, want 1: %+v", len(got.Input.Media), got.Input.Media)
	}
	if got.Input.Media[0]["type"] != "refer" || got.Input.Media[0]["url"] != "https://example.com/image.png" {
		t.Fatalf("unexpected media: %+v", got.Input.Media[0])
	}
}

func TestConvertToAliKlingRequestMapsKlingOfficialVideoList(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:  "kling/kling-v3-omni-video-generation",
		Prompt: "continue the dance",
		Metadata: map[string]interface{}{
			"video_list": []interface{}{
				map[string]interface{}{
					"video_url":           "https://example.com/video.mp4",
					"refer_type":          "base",
					"keep_original_sound": "yes",
				},
			},
			"duration":     "10",
			"mode":         "pro",
			"audio":        false,
			"aspect_ratio": "1:1",
		},
	}

	got, err := adaptor.convertToAliKlingRequest(req.Model, req)
	if err != nil {
		t.Fatalf("convertToAliKlingRequest returned error: %v", err)
	}
	if got.Parameters == nil || got.Parameters.Duration != 10 || got.Parameters.Mode != "pro" || got.Parameters.AspectRatio != "1:1" {
		t.Fatalf("unexpected parameters: %+v", got.Parameters)
	}
	if got.Parameters.Audio == nil || *got.Parameters.Audio != false {
		t.Fatalf("audio = %v, want false", got.Parameters.Audio)
	}
	if len(got.Input.Media) != 1 {
		t.Fatalf("media length = %d, want 1: %+v", len(got.Input.Media), got.Input.Media)
	}
	video := got.Input.Media[0]
	if video["type"] != "base" || video["url"] != "https://example.com/video.mp4" || video["keep_original_sound"] != "yes" {
		t.Fatalf("unexpected video media: %+v", video)
	}
	imageCount, videoCount := countAliBillingMedia(got, req.Metadata)
	if imageCount != 0 || videoCount != 1 {
		t.Fatalf("media counts = image:%d video:%d, want image:0 video:1", imageCount, videoCount)
	}
}

func TestConvertToAliKlingRequestResolvesDurationFromTaskSubmitReq(t *testing.T) {
	adaptor := &TaskAdaptor{}
	duration := 6

	tests := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want int
	}{
		{
			name: "duration takes precedence over seconds",
			req: relaycommon.TaskSubmitReq{
				Model:    "kling/kling-v3-omni-video-generation",
				Prompt:   "make a video",
				Duration: &duration,
				Seconds:  "9",
			},
			want: 6,
		},
		{
			name: "seconds is used when duration is absent",
			req: relaycommon.TaskSubmitReq{
				Model:   "kling/kling-v3-omni-video-generation",
				Prompt:  "make a video",
				Seconds: "9",
			},
			want: 9,
		},
		{
			name: "zero duration defaults to five seconds",
			req: relaycommon.TaskSubmitReq{
				Model:  "kling/kling-v3-omni-video-generation",
				Prompt: "make a video",
			},
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adaptor.convertToAliKlingRequest(tt.req.Model, tt.req)

			if err != nil {
				t.Fatalf("convertToAliKlingRequest returned error: %v", err)
			}
			if got.Parameters == nil || got.Parameters.Duration != tt.want {
				t.Fatalf("duration = %+v, want %d", got.Parameters, tt.want)
			}
		})
	}
}

func TestBuildAliKlingOfficialVideoResponseUsesPublicTaskID(t *testing.T) {
	resp := buildAliKlingOfficialVideoResponse(
		"task_public",
		AliVideoResponse{
			RequestID: "req_123",
			Output: AliVideoOutput{
				TaskID:     "upstream_task",
				TaskStatus: "SUCCEEDED",
				VideoURL:   "https://example.com/video.mp4",
			},
		},
		model.TaskStatusSuccess,
		100,
		200,
		"",
		"",
	)

	if resp.Code != 0 {
		t.Fatalf("code = %d, want 0", resp.Code)
	}
	if resp.TaskID != "task_public" || resp.Data.TaskID != "task_public" {
		t.Fatalf("task id was not converted to public task id: %+v", resp)
	}
	if resp.Data.TaskStatus != "succeed" {
		t.Fatalf("task status = %q, want succeed", resp.Data.TaskStatus)
	}
	if resp.Data.TaskResult == nil || len(resp.Data.TaskResult.Videos) != 1 || resp.Data.TaskResult.Videos[0].URL == "" {
		t.Fatalf("missing video result: %+v", resp.Data.TaskResult)
	}
}
