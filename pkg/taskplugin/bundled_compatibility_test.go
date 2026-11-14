// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/plugins"
	"github.com/stretchr/testify/require"
)

// The recommended embedded version is tested, independently of the retained
// upstream originals. Expected wire fields and dimensions are literal protocol
// facts; no expected value is computed by a second call to the tested hook.
func recommendedBundle(t *testing.T, key string) *Package {
	t.Helper()
	raw, err := plugins.TaskSources.ReadFile("SOURCES.json")
	require.NoError(t, err)
	var index struct {
		Plugins []struct{ Key, Path, SHA256 string }
	}
	require.NoError(t, common.Unmarshal(raw, &index))
	for _, entry := range index.Plugins {
		if entry.Key == key {
			source, err := plugins.TaskSources.ReadFile(strings.TrimPrefix(entry.Path, "plugins/"))
			require.NoError(t, err)
			p, err := LoadJavaScript(bytes.NewReader(source), entry.SHA256)
			require.NoError(t, err)
			return p
		}
	}
	t.Fatal("missing bundle", key)
	return nil
}
func hookObject(t *testing.T, p *Package, hook string, args ...any) map[string]any {
	t.Helper()
	raw, err := p.Call(t.Context(), hook, args...)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.Unmarshal(raw, &out))
	return out
}
func videoModel(t *testing.T, p *Package) string {
	t.Helper()
	for _, m := range p.Manifest().Models {
		if strings.Contains(m, "veo-") {
			return m
		}
	}
	return p.Manifest().Models[0]
}
func driverInput(model, action string, body any) map[string]any {
	return map[string]any{"model": model, "upstreamModel": model, "action": action, "requestBody": body, "apiKey": "synthetic-only", "authHeader": "Bearer synthetic-only", "auth": map[string]any{"projectId": "synthetic"}, "baseUrl": "https://provider.invalid", "channelOther": "us-central1", "config": map[string]any{"project": "synthetic", "region": "us-central1"}, "upstream": map[string]any{"kind": "vendor"}, "usagePurpose": "facts", "files": []any{}}
}

func TestBundledKlingEstimateMatchesOutgoingMode(t *testing.T) {
	p := recommendedBundle(t, "kling")
	input := driverInput("kling-v2-5-turbo", "text_to_video", map[string]any{"duration": 5, "mode": "std", "metadata": map[string]any{"mode": "pro"}})
	descriptor := hookObject(t, p, "buildSubmitRequest", input)
	require.Equal(t, "pro", descriptor["body"].(map[string]any)["mode"])
	facts := hookObject(t, p, "extractUsage", input)
	require.Equal(t, 2.5, facts["units"], "5 seconds * .5 pro credits/second, including missing-final-usage fallback")
	for _, mode := range []any{"", nil} {
		input = driverInput("kling-v2-5-turbo", "text_to_video", map[string]any{"duration": 5, "mode": "pro", "metadata": map[string]any{"mode": mode}})
		descriptor = hookObject(t, p, "buildSubmitRequest", input)
		require.Equal(t, "std", descriptor["body"].(map[string]any)["mode"])
		facts = hookObject(t, p, "extractUsage", input)
		require.Equal(t, 1.5, facts["units"], "present empty/null metadata overrides the top-level mode exactly as the wire builder does")
	}
}

func TestBundledVideoScalarImagesAndResolutionReachWire(t *testing.T) {
	const dataURL = "data:image/png;base64,iVBORw0KGgo="
	for _, key := range []string{"doubao", "google", "vertex-ai"} {
		t.Run(key, func(t *testing.T) {
			p := recommendedBundle(t, key)
			model := videoModel(t, p)
			if key == "doubao" {
				model = "doubao-seedance-1-0-pro-250528"
			}
			for _, field := range []string{"input_reference", "image"} {
				request := map[string]any{"model": model, "prompt": "test", "seconds": 4, "resolution": "1080p", field: dataURL}
				decoded := hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": request}})
				require.Equal(t, "image_to_video", decoded["action"])
				input := driverInput(model, "image_to_video", decoded["requestBody"])
				descriptor := hookObject(t, p, "buildSubmitRequest", input)
				require.Equal(t, "image_to_video", descriptor["action"], "accepted image cannot silently become text-only")
				body := descriptor["body"].(map[string]any)
				if key == "doubao" {
					content := body["content"].([]any)
					require.Equal(t, "image_url", content[0].(map[string]any)["type"])
				} else {
					instance := body["instances"].([]any)[0].(map[string]any)
					image := instance["image"].(map[string]any)
					require.Equal(t, "iVBORw0KGgo=", image["bytesBase64Encoded"])
					require.Equal(t, "image/png", image["mimeType"])
					require.NotContains(t, image, "inlineData")
					params := body["parameters"].(map[string]any)
					require.Equal(t, "1080p", params["resolution"])
					facts := hookObject(t, p, "extractUsage", input)
					require.Equal(t, "1080p", facts["resolution"])
				}
			}
		})
	}
}

func TestBundledGoogleUsesVersionedRESTImageAndCount(t *testing.T) {
	p := recommendedBundle(t, "google")
	model := videoModel(t, p)
	input := driverInput(model, "text_to_video", map[string]any{"prompt": "test", "duration": 4, "metadata": map[string]any{"sampleCount": 2, "numberOfVideos": 9}})
	descriptor := hookObject(t, p, "buildSubmitRequest", input)
	params := descriptor["body"].(map[string]any)["parameters"].(map[string]any)
	require.Equal(t, float64(1), params["sampleCount"])
	require.NotContains(t, params, "numberOfVideos")
	decoded := hookObject(t, p, "native.predictLongRunning", map[string]any{"params": map[string]any{"model": model + ":predictLongRunning"}, "body": map[string]any{"kind": "json", "value": map[string]any{"instances": []any{map[string]any{"prompt": "test", "image": map[string]any{"mimeType": "image/png", "bytesBase64Encoded": "iVBORw0KGgo="}}}, "parameters": map[string]any{"durationSeconds": 4}}}})
	require.Equal(t, "image_to_video", decoded["action"])
	_, err := p.Call(t.Context(), "protocols.openai_video.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "image": "https://unsupported.invalid/image.png"}}})
	require.Error(t, err, "unhandled image URL must not be discarded")
}

func TestBundledVertexDurationMatchesSentMetadata(t *testing.T) {
	p := recommendedBundle(t, "vertex-ai")
	model := videoModel(t, p)
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "input": "test", "metadata": map[string]any{"durationSeconds": 4}}}})
	input := driverInput(model, "text_to_video", decoded["requestBody"])
	descriptor := hookObject(t, p, "buildSubmitRequest", input)
	require.Equal(t, float64(4), descriptor["body"].(map[string]any)["parameters"].(map[string]any)["durationSeconds"])
	facts := hookObject(t, p, "extractUsage", input)
	require.Equal(t, float64(4), facts["seconds"])
}

func TestBundledPresentersPreserveGatewayIdentityAndTerminalStatus(t *testing.T) {
	for _, key := range []string{"alibaba", "sora"} {
		t.Run(key, func(t *testing.T) {
			p := recommendedBundle(t, key)
			view := map[string]any{"task_id": "public-1", "status": "FAILURE", "fail_reason": "host timeout", "progress": "100%", "created_at": 1, "updated_at": 2, "properties": map[string]any{"origin_model_name": "client-model"}, "data": map[string]any{"id": "private-1", "status": "in_progress", "size": "1280x720", "output": map[string]any{"task_status": "RUNNING"}}}
			out := hookObject(t, p, "protocols.openai_video.render", map[string]any{}, view)
			require.Equal(t, "public-1", out["id"])
			require.Equal(t, "failed", out["status"])
			require.Equal(t, "host timeout", out["error"].(map[string]any)["message"])
		})
	}
	p := recommendedBundle(t, "jimeng")
	out := hookObject(t, p, "native.taskSubmitted", map[string]any{}, map[string]any{"task_id": "public-1", "data": map[string]any{"code": 10000, "data": map[string]any{"task_id": "private-1", "extra": "kept"}}})
	require.Equal(t, "public-1", out["data"].(map[string]any)["task_id"])
	require.Equal(t, "kept", out["data"].(map[string]any)["extra"])
}

func TestBundledSunoSinglePollConsumesActualBatchEnvelope(t *testing.T) {
	p := recommendedBundle(t, "sunoapi")
	out := hookObject(t, p, "parseTaskResult", map[string]any{"taskId": "supplier-1"}, map[string]any{"code": "success", "data": []any{map[string]any{"task_id": "supplier-1", "status": "SUCCESS", "data": []any{}}}})
	require.Equal(t, "SUCCESS", out["status"])
	require.Equal(t, "supplier-1", out["taskId"])
}

func TestBundledXAIKeyframesAndMeasuredState(t *testing.T) {
	p := recommendedBundle(t, "xai")
	model := "grok-imagine-video"
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "metadata": map[string]any{"keyframes": []any{map[string]any{"image": map[string]any{"url": "https://synthetic.invalid/image.png"}, "timestamp_s": 0}}}}}})
	require.Equal(t, "image_to_video", decoded["action"])
	out := hookObject(t, p, "extractUsageOnComplete", map[string]any{"action": "video_extension", "state": map[string]any{"extension_seconds": 2}}, map[string]any{"state": map[string]any{"output_resolution": "720p"}}, map[string]any{"status": "done", "video": map[string]any{"duration": 7}})
	require.Equal(t, "720p", out["resolution"])
	require.Equal(t, float64(5), out["input_video_seconds"], "7 total - 2 extension = 5 input seconds")
}

func TestBundledAlibabaRejectsConflictingDeliveredImageCount(t *testing.T) {
	p := recommendedBundle(t, "alibaba")
	_, err := p.Call(t.Context(), "extractUsageOnComplete", map[string]any{"model": "qwen-image", "upstreamModel": "qwen-image"}, map[string]any{}, map[string]any{"usage": map[string]any{"image_count": 1}, "output": map[string]any{"results": []any{map[string]any{"url": "https://synthetic.invalid/1.png"}, map[string]any{"url": "https://synthetic.invalid/2.png"}}}})
	require.Error(t, err, "two delivered images and reported one are conflicting evidence")
}

func TestBundledDoubaoSizeReachesWire(t *testing.T) {
	p := recommendedBundle(t, "doubao")
	input := driverInput("doubao-seedance-1-0-pro-250528", "text_to_video", map[string]any{"prompt": "test", "seconds": 5, "size": "1920x1080"})
	out := hookObject(t, p, "buildSubmitRequest", input)
	require.Equal(t, "1080p", out["body"].(map[string]any)["resolution"])
}

func TestBundledHailuoImageArraysReachV1Frames(t *testing.T) {
	p := recommendedBundle(t, "hailuo")
	for _, model := range []string{"MiniMax-Hailuo-2.3-Fast", "MiniMax-Hailuo-02"} {
		t.Run(model, func(t *testing.T) {
			images := []any{"https://synthetic.invalid/first.png"}
			if model == "MiniMax-Hailuo-02" {
				images = append(images, "https://synthetic.invalid/last.png")
			}
			out := hookObject(t, p, "buildSubmitRequest", driverInput(model, "image_to_video", map[string]any{"prompt": "test", "duration": 6, "images": images}))
			body := out["body"].(map[string]any)
			require.Equal(t, images[0], body["first_frame_image"])
			if len(images) == 2 {
				require.Equal(t, images[1], body["last_frame_image"])
			}
		})
	}
}

func TestBundledJimengSubmittedQueryKeyMatchesMergedImages(t *testing.T) {
	p := recommendedBundle(t, "jimeng")
	input := driverInput("jimeng_v30", "first_tail_to_video", map[string]any{"prompt": "test", "duration": 5, "images": []any{"aGVsbG8="}, "metadata": map[string]any{"image_urls": []any{"https://synthetic.invalid/last.png"}}})
	input["apiKey"] = "sk-synthetic-only"
	descriptor := hookObject(t, p, "buildSubmitRequest", input)
	var wire map[string]any
	require.NoError(t, common.UnmarshalJsonStr(descriptor["body"].(string), &wire))
	require.Equal(t, "jimeng_i2v_first_tail_v30", wire["req_key"])
	submit := hookObject(t, p, "parseSubmitResponse", input, map[string]any{"body": map[string]any{"code": 10000, "data": map[string]any{"task_id": "supplier-1"}}})
	require.Equal(t, "jimeng_i2v_first_tail_v30", submit["state"].(map[string]any)["req_key"])
}

func TestBundledSunoCountsOnlySelectedTaskSongs(t *testing.T) {
	p := recommendedBundle(t, "sunoapi")
	body := map[string]any{"code": "success", "data": []any{
		map[string]any{"task_id": "other", "status": "SUCCESS", "data": []any{map[string]any{"audio_url": "other"}}},
		map[string]any{"task_id": "supplier-1", "status": "SUCCESS", "data": []any{map[string]any{"audio_url": "one"}, map[string]any{"audio_url": "two"}}},
	}}
	out := hookObject(t, p, "extractUsageOnComplete", map[string]any{"taskId": "supplier-1", "action": "music"}, map[string]any{"taskId": "supplier-1"}, body)
	require.Equal(t, float64(2), out["clips"])
	require.Equal(t, "music", out["action"])
}

func TestBundledSunoPOSTPollKeepsTaskLocalCompletionFacts(t *testing.T) {
	host := NewHost()
	_, err := host.Install(recommendedBundle(t, "sunoapi"))
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "r"}, "sunoapi", "suno_music", "suno_music", "music")
	require.NoError(t, err)
	const upstream = `{"code":"success","data":[{"task_id":"supplier-1","status":"SUCCESS","action":"music","data":[{"audio_url":"one"},{"audio_url":"two"}]}]}`
	requests, authorized := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/suno/fetch", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		require.JSONEq(t, `{"ids":["supplier-1"]}`, string(raw))
		_, _ = io.WriteString(w, upstream)
	}))
	defer server.Close()
	channel, broker := localBroker(t, e, server)
	result, err := e.Poll(t.Context(), broker, channel, ExecutionTask{Binding: e.Binding(), UpstreamTaskID: "supplier-1"}, func(ExecutionBinding) error { authorized++; return nil })
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Result.Status)
	require.Equal(t, float64(2), result.ActualFacts["clips"])
	require.Equal(t, "music", result.ActualFacts["action"])
	require.JSONEq(t, `[{"audio_url":"one"},{"audio_url":"two"}]`, string(result.Data))
	require.Equal(t, upstream, string(result.UpstreamBody))
	require.Equal(t, 1, requests)
	require.Equal(t, 1, authorized)
}

func TestBundledResponsesPreserveRepeatedFramesAndResolution(t *testing.T) {
	const frame = "data:image/png;base64,iVBORw0KGgo="
	for _, key := range []string{"vertex-ai", "vidu"} {
		t.Run(key, func(t *testing.T) {
			p := recommendedBundle(t, key)
			model := "viduq3-pro"
			if key == "vertex-ai" {
				model = "gemini-omni-1.2-flash"
			}
			decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "input": "test", "images": []any{frame, frame}, "resolution": "1080p"}}})
			request := decoded["requestBody"].(map[string]any)
			require.Len(t, request["images"], 2, "repeated array entries represent distinct first and last positions")
			if key == "vidu" {
				require.Equal(t, "first_tail_to_video", decoded["action"])
				out := hookObject(t, p, "buildSubmitRequest", driverInput(model, decoded["action"].(string), request))
				require.Equal(t, "1080p", out["body"].(map[string]any)["resolution"])
			}
		})
	}
}

func TestBundledXAIRejectsUnmeasurableEditBeforeSubmission(t *testing.T) {
	p := recommendedBundle(t, "xai")
	_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("grok-imagine-video", "video_edit", map[string]any{"prompt": "test", "video": map[string]any{"url": "https://synthetic.invalid/input.mp4"}}))
	require.Error(t, err, "this SDK cannot measure output resolution and must not silently bill the 720p reservation as actual")
}

func TestBundledGoogleMetadataDurationMatchesWireAndFacts(t *testing.T) {
	p := recommendedBundle(t, "google")
	input := driverInput(videoModel(t, p), "text_to_video", map[string]any{"prompt": "test", "metadata": map[string]any{"durationSeconds": 4}})
	wire := hookObject(t, p, "buildSubmitRequest", input)["body"].(map[string]any)
	require.Equal(t, float64(4), wire["parameters"].(map[string]any)["durationSeconds"])
	facts := hookObject(t, p, "extractUsage", input)
	require.Equal(t, float64(4), facts["seconds"])
}

func TestBundledJimengMixedImagesAndExplicitRemoval(t *testing.T) {
	p := recommendedBundle(t, "jimeng")
	for _, images := range [][]any{{"https://synthetic.invalid/frame.png", "aGVsbG8="}, {"aGVsbG8=", "https://synthetic.invalid/frame.png"}} {
		input := driverInput("jimeng_v30", "first_tail_to_video", map[string]any{"prompt": "test", "images": images})
		input["apiKey"] = "sk-synthetic-only"
		desc := hookObject(t, p, "buildSubmitRequest", input)
		var wire map[string]any
		require.NoError(t, common.UnmarshalJsonStr(desc["body"].(string), &wire))
		require.Equal(t, []any{"https://synthetic.invalid/frame.png"}, wire["image_urls"])
		require.Equal(t, []any{"aGVsbG8="}, wire["binary_data_base64"])
	}
	input := driverInput("jimeng_vgfm_t2v_l20", "text_to_video", map[string]any{"prompt": "test", "images": []any{"https://synthetic.invalid/frame.png"}, "metadata": map[string]any{"image_urls": []any{}}})
	input["apiKey"] = "sk-synthetic-only"
	desc := hookObject(t, p, "buildSubmitRequest", input)
	var wire map[string]any
	require.NoError(t, common.UnmarshalJsonStr(desc["body"].(string), &wire))
	require.Equal(t, "jimeng_vgfm_t2v_l20", wire["req_key"], "explicit empty override removes the image, so the request is text-only")
	require.Equal(t, "text_to_video", desc["action"])
}

func TestBundledSoraCanonicalReferenceAndDuration(t *testing.T) {
	p := recommendedBundle(t, "sora")
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "sora-2", "body": map[string]any{"kind": "json", "value": map[string]any{"model": "sora-2", "input": "test", "image": "https://synthetic.invalid/frame.png"}}})
	desc := hookObject(t, p, "buildSubmitRequest", driverInput("sora-2", "image_to_video", decoded["requestBody"]))
	require.Equal(t, map[string]any{"image_url": "https://synthetic.invalid/frame.png"}, desc["body"].(map[string]any)["input_reference"])
	decoded = hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": "sora-2", "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "duration": 8}}})
	desc = hookObject(t, p, "buildSubmitRequest", driverInput("sora-2", "text_to_video", decoded["requestBody"]))
	body := desc["body"].(map[string]any)
	require.Equal(t, "8", body["seconds"])
	require.NotContains(t, body, "duration")
}

func TestBundledVertexCompletedOperationRequiresVideo(t *testing.T) {
	p := recommendedBundle(t, "vertex-ai")
	input := driverInput(videoModel(t, p), "text_to_video", map[string]any{})
	out := hookObject(t, p, "parseTaskResult", input, map[string]any{"name": "operation-1", "done": true, "response": map[string]any{"raiMediaFilteredReasons": []any{"filtered synthetic output"}}})
	require.Equal(t, "FAILURE", out["status"])
	require.Contains(t, out["reason"], "filtered synthetic output")
	out = hookObject(t, p, "parseTaskResult", input, map[string]any{"name": "operation-1", "done": true, "response": map[string]any{"videos": []any{map[string]any{"gcsUri": "gs://synthetic/video.mp4"}}}})
	require.Equal(t, "SUCCESS", out["status"])
}

func TestBundledProtocolReferenceActionsMatchForwardedMedia(t *testing.T) {
	for _, test := range []struct {
		key, model, protocol string
		fields               map[string]any
	}{
		{"doubao", "doubao-seedance-1-0-pro-250528", "openai_video", map[string]any{"images": []any{"https://synthetic.invalid/frame.png"}}},
		{"doubao", "doubao-seedance-1-0-pro-250528", "openai_responses", map[string]any{"metadata": map[string]any{"content": []any{map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://synthetic.invalid/video.mp4"}}}}}},
		{"hailuo", "MiniMax-H3", "openai_video", map[string]any{"metadata": map[string]any{"reference_video": "https://synthetic.invalid/video.mp4"}}},
		{"hailuo", "MiniMax-H3", "openai_responses", map[string]any{"metadata": map[string]any{"reference_video": "https://synthetic.invalid/video.mp4"}}},
		{"kling", "kling-v2-5-turbo", "openai_responses", map[string]any{"metadata": map[string]any{"image": "https://synthetic.invalid/frame.png"}}},
	} {
		t.Run(test.key+test.protocol, func(t *testing.T) {
			p := recommendedBundle(t, test.key)
			req := map[string]any{"model": test.model, "prompt": "test", "input": "test"}
			for k, v := range test.fields {
				req[k] = v
			}
			decoded := hookObject(t, p, "protocols."+test.protocol+".decodeRequest", map[string]any{"model": test.model, "upstreamModel": test.model, "body": map[string]any{"kind": "json", "value": req}})
			require.Equal(t, "image_to_video", decoded["action"])
		})
	}
}

func TestBundledImageAndVideoPreparationRejectUnsupportedContexts(t *testing.T) {
	t.Run("synchronous image has no host task id", func(t *testing.T) {
		p := recommendedBundle(t, "doubao")
		_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("doubao-seedream-4-0-250828", "text_to_image", map[string]any{"prompt": "test"}))
		require.Error(t, err)
	})
	t.Run("remix has no ownership checked origin", func(t *testing.T) {
		p := recommendedBundle(t, "sora")
		_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("sora-2", "remix", map[string]any{"prompt": "test"}))
		require.Error(t, err)
	})
	for _, key := range []string{"google", "vertex-ai"} {
		t.Run(key+" extra Veo image", func(t *testing.T) {
			p := recommendedBundle(t, key)
			model := videoModel(t, p)
			_, err := p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "input": "test", "images": []any{"data:image/png;base64,iVBORw0KGgo=", "data:image/png;base64,iVBORw0KGgoAA=="}}}})
			require.Error(t, err)
		})
	}
}

func TestBundledEffectiveVideoSettingsPreserveExplicitFields(t *testing.T) {
	t.Run("doubao resolution", func(t *testing.T) {
		p := recommendedBundle(t, "doubao")
		out := hookObject(t, p, "buildSubmitRequest", driverInput("doubao-seedance-1-0-pro-250528", "text_to_video", map[string]any{"prompt": "test", "seconds": 5, "resolution": "480p"}))
		require.Equal(t, "480p", out["body"].(map[string]any)["resolution"])
	})
	t.Run("doubao fractional duration", func(t *testing.T) {
		p := recommendedBundle(t, "doubao")
		_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("doubao-seedance-1-0-pro-250528", "text_to_video", map[string]any{"prompt": "test", "seconds": 0.5}))
		require.Error(t, err)
	})
	t.Run("vidu metadata size", func(t *testing.T) {
		p := recommendedBundle(t, "vidu")
		out := hookObject(t, p, "buildSubmitRequest", driverInput("viduq3-pro", "text_to_video", map[string]any{"prompt": "test", "metadata": map[string]any{"size": "1920x1080"}}))
		require.Equal(t, "1080p", out["body"].(map[string]any)["resolution"])
	})
	t.Run("xai top seconds overrides metadata duration", func(t *testing.T) {
		p := recommendedBundle(t, "xai")
		input := driverInput("grok-imagine-video", "text_to_video", map[string]any{"prompt": "test", "seconds": 4, "metadata": map[string]any{"duration": 12}})
		out := hookObject(t, p, "buildSubmitRequest", input)
		require.Equal(t, float64(4), out["body"].(map[string]any)["duration"])
		facts := hookObject(t, p, "extractUsage", input)
		require.Equal(t, float64(4), facts["seconds"])
	})
}

func TestBundledHailuoRateLimitedPollIsRetryable(t *testing.T) {
	p := recommendedBundle(t, "hailuo")
	_, err := p.Call(t.Context(), "parseTaskResult", driverInput("MiniMax-Hailuo-02", "text_to_video", map[string]any{}), map[string]any{"base_resp": map[string]any{"status_code": 1002, "status_msg": "synthetic rate limit"}})
	require.Error(t, err, "a transient poll must not terminally fail an accepted task")
}

func TestBundledAlibabaNativeActionsAndCreateIdentity(t *testing.T) {
	p := recommendedBundle(t, "alibaba")
	const model = "qwen-image-2.0"
	input := map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"text": "test"}, map[string]any{"image": "https://synthetic.invalid/frame.png"}}}}}
	decoded := hookObject(t, p, "native.createImageTask", map[string]any{"model": model, "upstreamModel": model, "path": "/api/v1/services/aigc/multimodal-generation/generation", "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "input": input}}})
	require.Equal(t, "image_to_image", decoded["action"])
	decoded = hookObject(t, p, "protocols.openai_image.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "prompt": "test", "input": input}}})
	require.Equal(t, "image_to_image", decoded["action"])
	out := hookObject(t, p, "native.taskCreated", map[string]any{}, map[string]any{"task_id": "public-1", "data": map[string]any{"output": map[string]any{"task_id": "private-1"}}})
	require.Equal(t, "public-1", out["output"].(map[string]any)["task_id"])
	decoded = hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": "wan3.0-video", "upstreamModel": "wan3.0-video", "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "audio_url": "https://synthetic.invalid/audio.mp3"}}})
	require.Equal(t, "image_to_video", decoded["action"], "audio shorthand must classify exactly as forwarded reference media")
}
