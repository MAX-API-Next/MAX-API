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
	input := driverInput(model, "text_to_video", map[string]any{"prompt": "test", "duration": 4, "metadata": map[string]any{"sampleCount": 1, "numberOfVideos": 1}})
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
	input := driverInput("jimeng_v30", "first_tail_to_video", map[string]any{"prompt": "test", "duration": 5, "images": []any{"https://synthetic.invalid/ignored.png"}, "metadata": map[string]any{"image_urls": []any{"https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"}}})
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
		_, err := p.Call(t.Context(), "buildSubmitRequest", input)
		require.Error(t, err, "both frame orders must fail before a request can lose their positions")
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
		{"hailuo", "MiniMax-Hailuo-2.3", "openai_responses", map[string]any{"metadata": map[string]any{"first_frame_image": "https://synthetic.invalid/frame.png"}}},
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

func TestBundledAlibabaEffectiveOverrideAndRepeatedFrames(t *testing.T) {
	p := recommendedBundle(t, "alibaba")
	decoded := hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": "wan3.0-video", "upstreamModel": "wan3.0-video", "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "media": []any{map[string]any{"type": "reference_image", "url": "https://synthetic.invalid/frame.png"}}, "metadata": map[string]any{"input": map[string]any{"media": []any{}}}}}})
	require.Equal(t, "text_to_video", decoded["action"], "explicit empty effective media replaces top-level media")
	decoded = hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "wan2.2-kf2v-flash", "upstreamModel": "wan2.2-kf2v-flash", "body": map[string]any{"kind": "json", "value": map[string]any{"model": "wan2.2-kf2v-flash", "input": "test", "images": []any{"https://synthetic.invalid/frame.png", "https://synthetic.invalid/frame.png"}}}})
	require.Len(t, decoded["requestBody"].(map[string]any)["images"], 2)
}

func TestBundledGoogleFilteredCompletionHasNoSuccessfulDelivery(t *testing.T) {
	p := recommendedBundle(t, "google")
	input := driverInput(videoModel(t, p), "text_to_video", map[string]any{})
	body := map[string]any{"name": "operation-1", "done": true, "response": map[string]any{"generateVideoResponse": map[string]any{"raiMediaFilteredCount": 1, "raiMediaFilteredReasons": []any{"synthetic filter"}}}}
	out := hookObject(t, p, "parseTaskResult", input, body)
	require.Equal(t, "FAILURE", out["status"])
	out = hookObject(t, p, "parseSubmitResponse", input, map[string]any{"body": body})
	require.Equal(t, "FAILURE", out["immediate"].(map[string]any)["status"])
}

func TestBundledJimengDecoderUsesEffectiveKeyAndImageFields(t *testing.T) {
	p := recommendedBundle(t, "jimeng")
	model := "jimeng_vgfm_t2v_l20"
	decoded := hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "images": []any{"https://synthetic.invalid/old.png"}, "metadata": map[string]any{"image_urls": []any{"https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"}}}}})
	require.Equal(t, "first_tail_to_video", decoded["action"])
	decoded = hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "seconds": 10, "metadata": map[string]any{"req_key": "jimeng_v30"}}}})
	require.Equal(t, float64(10), decoded["requestBody"].(map[string]any)["duration"])
}

func TestBundledKlingCreateSnapshotCarriesPendingStatus(t *testing.T) {
	p := recommendedBundle(t, "kling")
	out := hookObject(t, p, "native.taskStatus", map[string]any{}, map[string]any{"task_id": "public-1", "status": "SUBMITTED", "data": map[string]any{"code": 0, "data": map[string]any{"task_id": "private-1"}}})
	require.Equal(t, "submitted", out["data"].(map[string]any)["task_status"])
	require.Equal(t, "public-1", out["data"].(map[string]any)["task_id"])
}

func TestBundledViduUnsupportedActionDoesNotBecomeTextGeneration(t *testing.T) {
	p := recommendedBundle(t, "vidu")
	_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("viduq3-pro", "remix", map[string]any{"prompt": "test", "metadata": map[string]any{"action": "remixGenerate"}}))
	require.Error(t, err)
}

func TestBundledXAIMultipartMetadataPreservesExplicitValues(t *testing.T) {
	p := recommendedBundle(t, "xai")
	out := hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": "grok-imagine-video", "upstreamModel": "grok-imagine-video", "body": map[string]any{"kind": "multipart", "fields": map[string]any{"prompt": []string{"test"}, "metadata": []string{`{"duration":4,"resolution":"720p","generate_audio":false}`}}}})
	body := out["requestBody"].(map[string]any)
	require.Equal(t, float64(4), body["duration"])
	require.Equal(t, "720p", body["resolution"])
	require.Equal(t, false, body["generate_audio"], "an explicit false in multipart metadata is not a missing value")
}

func TestDeclaredUnavailableVendorCapabilitiesRemainClosed(t *testing.T) {
	t.Run("MiniMax direct file lookup", func(t *testing.T) {
		p := recommendedBundle(t, "hailuo")
		_, err := p.Call(t.Context(), "buildContentRequest", map[string]any{"model": "MiniMax-Hailuo-02", "upstreamModel": "MiniMax-Hailuo-02", "data": map[string]any{"file_id": "synthetic-file"}, "artifactKey": "video", "baseUrl": "https://provider.invalid", "apiKey": "synthetic-only", "upstream": map[string]any{"kind": "vendor"}, "clientRequest": map[string]any{"method": "GET"}})
		require.Error(t, err, "utils.fetch is explicitly unavailable; no artifact delivery is claimed")
	})
	t.Run("Jimeng direct V4 signing", func(t *testing.T) {
		host := NewHost()
		_, err := host.Install(recommendedBundle(t, "jimeng"))
		require.NoError(t, err)
		e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "r"}, "jimeng", "jimeng_vgfm_t2v_l20", "jimeng_vgfm_t2v_l20", "text_to_video")
		require.NoError(t, err)
		_, err = e.Prepare(t.Context(), ChannelInput{ID: 33, BaseURL: "https://provider.invalid", APIKey: "synthetic-ak|synthetic-sk", UpstreamKind: "vendor"}, []byte(`{"prompt":"test","duration":5}`))
		require.Error(t, err, "unimplemented signing cannot reach host authorization or dispatch")
	})
}

func TestBundledAlibabaNativeOSSConsentReachesWire(t *testing.T) {
	p := recommendedBundle(t, "alibaba")
	model := "qwen-image-2.0"
	decoded := hookObject(t, p, "native.createImageTask", map[string]any{"model": model, "upstreamModel": model, "path": "/api/v1/services/aigc/multimodal-generation/generation", "requestHeaders": map[string]any{"X-DashScope-OssResourceResolve": "enable"}, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"text": "test"}, map[string]any{"image": "oss://synthetic/frame.png"}}}}}}}})
	out := hookObject(t, p, "buildSubmitRequest", driverInput(model, "image_to_image", decoded["requestBody"]))
	require.Equal(t, "enable", out["headers"].(map[string]any)["X-DashScope-OssResourceResolve"])
}

func TestBundledKlingRejectsCallbackFieldsFromMetadata(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, key := range []string{"callback_url", "external_task_id"} {
		_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("kling-v2-5-turbo", "text_to_video", map[string]any{"prompt": "test", "duration": 5, "metadata": map[string]any{key: "synthetic"}}))
		require.Error(t, err, key)
	}
}

func TestBundledViduMetadataImagesMatchActionAndEndpoint(t *testing.T) {
	p := recommendedBundle(t, "vidu")
	model := "viduq3-pro"
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "upstreamModel": model, "body": map[string]any{"kind": "json", "value": map[string]any{"model": model, "input": "test", "metadata": map[string]any{"images": []any{"https://synthetic.invalid/frame.png"}}}}})
	require.Equal(t, "image_to_video", decoded["action"])
	out := hookObject(t, p, "buildSubmitRequest", driverInput(model, "image_to_video", decoded["requestBody"]))
	require.Contains(t, out["url"], "/img2video")
	require.Len(t, out["body"].(map[string]any)["images"], 1)
}

func TestBundledXAIOrdinaryMissingVideoFailsButModerationKeepsPolicy(t *testing.T) {
	p := recommendedBundle(t, "xai")
	input := driverInput("grok-imagine-video", "text_to_video", map[string]any{})
	for _, tc := range []struct {
		moderated bool
		status    string
	}{{false, "FAILURE"}, {true, "SUCCESS"}} {
		snapshot := map[string]any{"status": "done", "video": map[string]any{"respect_moderation": !tc.moderated, "duration": float64(8)}}
		out := hookObject(t, p, "parseTaskResult", input, snapshot)
		require.Equal(t, tc.status, out["status"])
		view := hookObject(t, p, "native.videoStatus", map[string]any{}, map[string]any{"status": out["status"], "fail_reason": out["reason"], "data": snapshot})
		if tc.moderated {
			require.Equal(t, snapshot, view, "intentional moderation retains the successful host policy and provider result")
		} else {
			require.Equal(t, "failed", view["status"])
			failure, ok := view["error"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "completed video result has no video URL", failure["message"])
		}
	}
}

func TestBundledDoubaoMetadataPreservesTextAndRejectsCallback(t *testing.T) {
	p := recommendedBundle(t, "doubao")
	model := "doubao-seedance-1-0-pro-250528"
	_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput(model, "text_to_video", map[string]any{"prompt": "test", "metadata": map[string]any{"callback_url": "https://synthetic.invalid/callback"}}))
	require.Error(t, err)
	out := hookObject(t, p, "buildSubmitRequest", driverInput(model, "image_to_video", map[string]any{"metadata": map[string]any{"content": []any{map[string]any{"type": "text", "text": "keep my instructions"}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://synthetic.invalid/frame.png"}}}}}))
	content := out["body"].(map[string]any)["content"].([]any)
	require.Contains(t, content, map[string]any{"type": "text", "text": "keep my instructions"})
	req := map[string]any{"prompt": "replacement instructions", "metadata": map[string]any{"content": content}}
	out = hookObject(t, p, "buildSubmitRequest", driverInput(model, "image_to_video", req))
	content = out["body"].(map[string]any)["content"].([]any)
	require.Contains(t, content, map[string]any{"type": "text", "text": "replacement instructions"})
	require.NotContains(t, content, map[string]any{"type": "text", "text": "keep my instructions"})
}

func TestBundledDoubaoRatioUsesEffectiveResolution(t *testing.T) {
	p := recommendedBundle(t, "doubao")
	input := driverInput("doubao-seedance-2-0-260128", "text_to_video", map[string]any{"prompt": "test", "resolution": "1080p"})
	input["usagePurpose"] = "billing_ratios"
	out := hookObject(t, p, "extractUsage", input)
	require.InDelta(t, 51.0/46.0, out["video_input_ratio"], 1e-12)
}

func TestBundledVideoCountMustBeExplicitlySupported(t *testing.T) {
	for _, key := range []string{"google", "vertex-ai"} {
		t.Run(key, func(t *testing.T) {
			p := recommendedBundle(t, key)
			for _, field := range []string{"sampleCount", "numberOfVideos"} {
				for _, count := range []any{2, 0, nil} {
					_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput(videoModel(t, p), "text_to_video", map[string]any{"prompt": "test", "metadata": map[string]any{field: count}}))
					require.Error(t, err, "one-video host must reject unsupported explicit quantities rather than silently rewrite them")
				}
			}
			for _, metadata := range []any{map[string]any{}, map[string]any{"sampleCount": 1, "numberOfVideos": 1}} {
				out := hookObject(t, p, "buildSubmitRequest", driverInput(videoModel(t, p), "text_to_video", map[string]any{"prompt": "test", "metadata": metadata}))
				params := out["body"].(map[string]any)["parameters"].(map[string]any)
				require.Equal(t, float64(1), params["sampleCount"])
				require.NotContains(t, params, "numberOfVideos")
			}
			if key == "vertex-ai" {
				model := videoModel(t, p)
				decoded := hookObject(t, p, "native.modelMethod", map[string]any{"params": map[string]any{"model": model + ":predictLongRunning"}, "body": map[string]any{"kind": "json", "value": map[string]any{"instances": []any{map[string]any{"prompt": "test"}}, "parameters": map[string]any{"sampleCount": 2}}}})
				_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput(model, "text_to_video", decoded["requestBody"]))
				require.Error(t, err, "native parameters must reach the same pre-submission quantity guard")
			}
		})
	}
}

func TestBundledViduRendererKeepsOriginalModel(t *testing.T) {
	p := recommendedBundle(t, "vidu")
	out := hookObject(t, p, "protocols.openai_video.render", map[string]any{}, map[string]any{"task_id": "public-1", "status": "SUBMITTED", "properties": map[string]any{"origin_model_name": "logical-vidu"}})
	require.Equal(t, "logical-vidu", out["model"])
}

func TestBundledKlingVideoRejectsUnsupportedImageLists(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, images := range []any{[]any{"https://synthetic.invalid/first.png"}, []any{"https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"}, "not-an-array"} {
		_, err := p.Call(t.Context(), "protocols.openai_video.decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "images": images}}})
		require.Error(t, err, "unsupported list input must not become an accepted text-only task")
	}
	for _, tc := range []struct {
		fields map[string]any
		action string
	}{{map[string]any{"images": []any{}}, "text_to_video"}, {map[string]any{"image": "https://synthetic.invalid/first.png"}, "image_to_video"}, {map[string]any{"input_reference": "https://synthetic.invalid/first.png"}, "image_to_video"}} {
		fields := tc.fields
		fields["prompt"] = "test"
		out := hookObject(t, p, "protocols.openai_video.decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": fields}})
		require.Equal(t, tc.action, out["action"])
	}
}

func TestBundledXAIRejectsConflictingImageRepresentations(t *testing.T) {
	p := recommendedBundle(t, "xai")
	for _, fields := range []map[string]any{{"image": "https://synthetic.invalid/scalar.png"}, {"input_reference": "https://synthetic.invalid/scalar.png"}, {"metadata": map[string]any{"image": "https://synthetic.invalid/scalar.png"}}, {"reference_images": []any{"https://synthetic.invalid/scalar.png"}}} {
		fields["prompt"], fields["images"] = "test", []any{"https://synthetic.invalid/listed.png"}
		_, err := p.Call(t.Context(), "buildSubmitRequest", driverInput("grok-imagine-video", "image_to_video", fields))
		require.Error(t, err, "a mixed scalar/reference-list and legacy list must fail instead of silently losing media")
		_, err = p.Call(t.Context(), "extractUsage", driverInput("grok-imagine-video", "image_to_video", fields))
		require.Error(t, err, "quote preparation must use the same rejection contract")
		fields["model"], fields["input"] = "grok-imagine-video", "test"
		_, err = p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": "grok-imagine-video", "body": map[string]any{"kind": "json", "value": fields}})
		require.Error(t, err, "Responses must reject conflicts before normalization hides the original representations")
	}
	for _, images := range [][]any{{"https://synthetic.invalid/first.png"}, {"https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"}} {
		out := hookObject(t, p, "buildSubmitRequest", driverInput("grok-imagine-video", "image_to_video", map[string]any{"prompt": "test", "images": images}))
		body := out["body"].(map[string]any)
		if len(images) == 1 {
			require.NotNil(t, body["image"])
		} else {
			require.Len(t, body["reference_images"], 2)
		}
		decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "grok-imagine-video", "body": map[string]any{"kind": "json", "value": map[string]any{"model": "grok-imagine-video", "input": "test", "images": images}}})
		action, ok := decoded["action"].(string)
		require.True(t, ok)
		out = hookObject(t, p, "buildSubmitRequest", driverInput("grok-imagine-video", action, decoded["requestBody"]))
		require.Equal(t, body, out["body"], "valid list-only input must retain every image through Responses conversion")
	}
}

func TestBundledKlingActionUsesEffectiveMergedImages(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, protocol := range []string{"openai_video", "openai_responses"} {
		for _, image := range []any{"", nil, "https://synthetic.invalid/replacement.png"} {
			fields := map[string]any{"model": "kling-v2-5-turbo", "prompt": "test", "input": "test", "image": "https://synthetic.invalid/original.png", "metadata": map[string]any{"image": image}}
			decoded := hookObject(t, p, "protocols."+protocol+".decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": fields}})
			action, endpoint := "text_to_video", "/v1/videos/text2video"
			if image != nil && image != "" {
				action, endpoint = "image_to_video", "/v1/videos/image2video"
			}
			require.Equal(t, action, decoded["action"], "%s image=%v", protocol, image)
			decodedAction, ok := decoded["action"].(string)
			require.True(t, ok)
			out := hookObject(t, p, "buildSubmitRequest", driverInput("kling-v2-5-turbo", decodedAction, decoded["requestBody"]))
			require.Equal(t, action, out["action"])
			require.Equal(t, "https://provider.invalid"+endpoint, out["url"])
			body := out["body"].(map[string]any)
			if action == "text_to_video" {
				require.NotContains(t, body, "image")
			} else {
				require.Equal(t, image, body["image"])
			}
		}
	}
}

func TestBundledKlingResponsesRejectExcessFrames(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const first, last, extra = "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png", "https://synthetic.invalid/extra.png"
	for _, fields := range []map[string]any{
		{"images": []any{first, last, extra}},
		{"image": first, "images": []any{last, extra}},
		{"images": []any{first, last}, "input": []any{map[string]any{"type": "input_image", "image_url": extra}}},
	} {
		fields["model"], fields["prompt"] = "kling-v2-5-turbo", "test"
		_, err := p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": fields}})
		require.Error(t, err, "excess media must be rejected before selecting first and last frames")
	}
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": map[string]any{"input": "test", "images": []any{first, last}}}})
	action, ok := decoded["action"].(string)
	require.True(t, ok)
	out := hookObject(t, p, "buildSubmitRequest", driverInput("kling-v2-5-turbo", action, decoded["requestBody"]))
	body := out["body"].(map[string]any)
	require.Equal(t, first, body["image"])
	require.Equal(t, last, body["image_tail"])
}

func TestBundledKlingResponsesMetadataCannotDiscardDistinctFrames(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const first, last, extra = "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png", "https://synthetic.invalid/extra.png"
	for _, metadata := range []map[string]any{{"image_tail": extra}, {"image": extra}, {"image_tail": first}, {"image": last}, {"image": ""}} {
		_, err := p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": map[string]any{"input": "test", "images": []any{first, last}, "metadata": metadata}}})
		require.Error(t, err, "metadata must not silently replace one of two distinct submitted frames")
	}
	for _, metadata := range []map[string]any{{"image_tail": last}, {"image": last, "image_tail": first}} {
		decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": map[string]any{"input": "test", "images": []any{first, last}, "metadata": metadata}}})
		action, ok := decoded["action"].(string)
		require.True(t, ok)
		out := hookObject(t, p, "buildSubmitRequest", driverInput("kling-v2-5-turbo", action, decoded["requestBody"]))
		body := out["body"].(map[string]any)
		require.ElementsMatch(t, []any{first, last}, []any{body["image"], body["image_tail"]})
	}
}

func TestBundledKling4UsesEffectiveResponseFrames(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const first, last = "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "kling-4.0", "body": map[string]any{"kind": "json", "value": map[string]any{"input": "test", "images": []any{first, last}, "metadata": map[string]any{"image": last, "image_tail": first}}}})
	action, ok := decoded["action"].(string)
	require.True(t, ok)
	out := hookObject(t, p, "buildSubmitRequest", driverInput("kling-4.0", action, decoded["requestBody"]))
	body := out["body"].(map[string]any)
	require.Equal(t, []any{map[string]any{"type": "prompt", "text": "test"}, map[string]any{"type": "first_frame", "url": last}, map[string]any{"type": "last_frame", "url": first}}, body["contents"])
}

func TestBundledXAIResponsesPreservesObjectImages(t *testing.T) {
	p := recommendedBundle(t, "xai")
	const first, last = "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"
	for _, fields := range []map[string]any{{"image": map[string]any{"url": first}}, {"input_reference": map[string]any{"image_url": first}}, {"images": []any{map[string]any{"url": first}, map[string]any{"image_url": last}}}} {
		fields["model"], fields["input"] = "grok-imagine-video", "test"
		decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "grok-imagine-video", "body": map[string]any{"kind": "json", "value": fields}})
		action, ok := decoded["action"].(string)
		require.True(t, ok)
		require.NotEqual(t, "text_to_video", action)
		out := hookObject(t, p, "buildSubmitRequest", driverInput("grok-imagine-video", action, decoded["requestBody"]))
		body := out["body"].(map[string]any)
		if _, listed := fields["images"]; listed {
			require.Equal(t, []any{map[string]any{"url": first}, map[string]any{"url": last}}, body["reference_images"])
		} else {
			require.Equal(t, map[string]any{"url": first}, body["image"])
		}
	}
	for _, image := range []any{map[string]any{}, map[string]any{"file_id": "unsupported-private-file"}, 42} {
		_, err := p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": "grok-imagine-video", "body": map[string]any{"kind": "json", "value": map[string]any{"model": "grok-imagine-video", "input": "test", "image": image}}})
		require.Error(t, err, "invalid image representations must not silently become a text-only task")
	}
}

func TestBundledKling4RejectsMetadataContentsBeforePreparation(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const first, last = "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"
	decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": "kling-4.0", "body": map[string]any{"kind": "json", "value": map[string]any{"input": "test", "images": []any{first, last}, "metadata": map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "replacement"}}}}}})
	action, ok := decoded["action"].(string)
	require.True(t, ok)
	for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
		_, err := p.Call(t.Context(), hook, driverInput("kling-4.0", action, decoded["requestBody"]))
		require.Error(t, err, "conflicting contents must fail before request or quote preparation")
	}
	nativeBody := map[string]any{"contents": []any{map[string]any{"type": "first_frame", "url": first}}}
	out := hookObject(t, p, "buildSubmitRequest", driverInput("kling-4.0", "image_to_video", map[string]any{"endpoint": "image-to-video", "vendorBody": nativeBody}))
	require.Equal(t, nativeBody, out["body"], "native contents remain a supported explicit representation")
}

func TestBundledKlingResponsesKeepsRepeatedFramePositions(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const image = "https://synthetic.invalid/repeated.png"
	for _, model := range []string{"kling-v2-5-turbo", "kling-4.0"} {
		for _, fields := range []map[string]any{
			{"images": []any{image, image}},
			{"image": image, "input_reference": image, "images": []any{image, image}},
			{"input": []any{map[string]any{"type": "input_image", "image_url": image}, map[string]any{"type": "input_image", "image_url": image}}},
		} {
			fields["prompt"] = "test"
			decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": fields}})
			action, ok := decoded["action"].(string)
			require.True(t, ok)
			out := hookObject(t, p, "buildSubmitRequest", driverInput(model, action, decoded["requestBody"]))
			body := out["body"].(map[string]any)
			if model == "kling-4.0" {
				require.Equal(t, []any{map[string]any{"type": "prompt", "text": "test"}, map[string]any{"type": "first_frame", "url": image}, map[string]any{"type": "last_frame", "url": image}}, body["contents"])
			} else {
				require.Equal(t, image, body["image"])
				require.Equal(t, image, body["image_tail"])
			}
		}
		_, err := p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "images": []any{image, image, image}}}})
		require.Error(t, err, "three ordered frame positions cannot fit a first/tail request even when their URLs match")
	}
}

func TestBundledKlingCountsFramesAfterSingleImageOverride(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const old, first, last = "https://synthetic.invalid/old.png", "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"
	for _, model := range []string{"kling-v2-5-turbo", "kling-4.0"} {
		decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "image": old, "metadata": map[string]any{"image": first, "image_tail": last}}}})
		action, ok := decoded["action"].(string)
		require.True(t, ok)
		out := hookObject(t, p, "buildSubmitRequest", driverInput(model, action, decoded["requestBody"]))
		body := out["body"].(map[string]any)
		if model == "kling-4.0" {
			require.Equal(t, []any{map[string]any{"type": "prompt", "text": "test"}, map[string]any{"type": "first_frame", "url": first}, map[string]any{"type": "last_frame", "url": last}}, body["contents"])
		} else {
			require.Equal(t, first, body["image"])
			require.Equal(t, last, body["image_tail"])
		}
	}
}

func TestBundledKlingNormalizesWhitespaceTailBeforeBuilding(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const first, last = "https://synthetic.invalid/first.png", "https://synthetic.invalid/last.png"
	for _, model := range []string{"kling-v3", "kling-4.0"} {
		decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "images": []any{first, last}, "metadata": map[string]any{"image_tail": " "}}}})
		action, ok := decoded["action"].(string)
		require.True(t, ok)
		out := hookObject(t, p, "buildSubmitRequest", driverInput(model, action, decoded["requestBody"]))
		body := out["body"].(map[string]any)
		if model == "kling-4.0" {
			require.Equal(t, []any{map[string]any{"type": "prompt", "text": "test"}, map[string]any{"type": "first_frame", "url": first}, map[string]any{"type": "last_frame", "url": last}}, body["contents"])
		} else {
			require.Equal(t, last, body["image_tail"])
		}
	}
}

func TestBundledKlingVideoRendererKeepsOriginalModel(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, model := range []string{"logical-kling-alias", "kling-v3", "kling-4.0"} {
		out := hookObject(t, p, "protocols.openai_video.render", map[string]any{"model": "current-channel-model"}, map[string]any{"task_id": "public-1", "status": "SUBMITTED", "properties": map[string]any{"origin_model_name": model}})
		require.Equal(t, model, out["model"], "historical task identity wins over the current channel")
	}
}

func TestBundledKlingVideoRendererConvertsProviderMilliseconds(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, data := range []map[string]any{
		{"id": "vendor-1", "status": "succeeded", "create_time": 1714000000123, "update_time": 1714000009876},
		{"task_id": "vendor-1", "task_status": "succeed", "created_at": 1714000000123, "updated_at": 1714000009876},
	} {
		out := hookObject(t, p, "protocols.openai_video.render", map[string]any{}, map[string]any{"task_id": "public-1", "status": "SUCCESS", "data": map[string]any{"code": 0, "data": data}})
		require.Equal(t, float64(1714000000), out["created_at"])
		require.Equal(t, float64(1714000009), out["completed_at"])
	}
}

func TestBundledKlingVideoRendererCompletesOnlyTerminalTasks(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, data := range []map[string]any{
		{"id": "vendor-1", "status": "processing", "create_time": 1714000000123, "update_time": 1714000009876},
		{"task_id": "vendor-1", "task_status": "processing", "created_at": 1714000000123, "updated_at": 1714000009876},
	} {
		for _, status := range []string{"IN_PROGRESS", "SUCCESS", "FAILURE"} {
			out := hookObject(t, p, "protocols.openai_video.render", map[string]any{}, map[string]any{"task_id": "public-1", "status": status, "data": map[string]any{"code": 0, "data": data}})
			if status == "IN_PROGRESS" {
				require.NotContains(t, out, "completed_at")
			} else {
				require.Equal(t, float64(1714000009), out["completed_at"])
			}
		}
	}
}

func TestBundledKlingRequiresPromptOrEffectiveFrames(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const image = "https://synthetic.invalid/first.png"
	for _, model := range []string{"kling-v3", "kling-4.0"} {
		decoded := hookObject(t, p, "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"metadata": map[string]any{"image": image}}}})
		action, ok := decoded["action"].(string)
		require.True(t, ok)
		require.Equal(t, "image_to_video", action)
		out := hookObject(t, p, "buildSubmitRequest", driverInput(model, action, decoded["requestBody"]))
		body := out["body"].(map[string]any)
		if model == "kling-4.0" {
			require.Equal(t, []any{map[string]any{"type": "first_frame", "url": image}}, body["contents"])
		} else {
			require.Equal(t, image, body["image"])
		}
		for _, empty := range []any{nil, "", " "} {
			_, err := p.Call(t.Context(), "protocols.openai_responses.decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"image": image, "metadata": map[string]any{"image": empty}}}})
			require.Error(t, err, "an override removing the only image without a prompt must fail")
		}
	}
}

func TestBundledKlingSingleImageDropsBlankTail(t *testing.T) {
	p := recommendedBundle(t, "kling")
	const image = "https://synthetic.invalid/first.png"
	for _, model := range []string{"kling-v3", "kling-4.0"} {
		for _, tail := range []any{nil, "", " "} {
			for _, protocol := range []string{"openai_responses", "openai_video"} {
				fields := map[string]any{"prompt": "test", "image": image, "metadata": map[string]any{"image_tail": tail}}
				decoded := hookObject(t, p, "protocols."+protocol+".decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": fields}})
				action, ok := decoded["action"].(string)
				require.True(t, ok)
				out := hookObject(t, p, "buildSubmitRequest", driverInput(model, action, decoded["requestBody"]))
				body := out["body"].(map[string]any)
				if model == "kling-4.0" {
					require.Equal(t, []any{map[string]any{"type": "prompt", "text": "test"}, map[string]any{"type": "first_frame", "url": image}}, body["contents"])
				} else {
					require.NotContains(t, body, "image_tail")
				}
			}
		}
	}
}

func TestBundledKlingBlankMetadataImageClearsShorthand(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, protocol := range []string{"openai_responses", "openai_video"} {
		for _, model := range []string{"kling-v3", "kling-4.0"} {
			decoded := hookObject(t, p, "protocols."+protocol+".decodeRequest", map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "image": "https://synthetic.invalid/old.png", "metadata": map[string]any{"image": " "}}}})
			action, ok := decoded["action"].(string)
			require.True(t, ok)
			require.Equal(t, "text_to_video", action)
			out := hookObject(t, p, "buildSubmitRequest", driverInput(model, action, decoded["requestBody"]))
			body := out["body"].(map[string]any)
			require.Equal(t, "text_to_video", out["action"])
			require.NotContains(t, body, "image")
			require.NotContains(t, body, "contents")
		}
	}
}

func TestBundledKlingProtocolModePrecedenceMatchesQuote(t *testing.T) {
	p := recommendedBundle(t, "kling")
	for _, protocol := range []string{"openai_responses", "openai_video"} {
		for _, mode := range []any{"std", nil, ""} {
			decoded := hookObject(t, p, "protocols."+protocol+".decodeRequest", map[string]any{"model": "kling-v2-5-turbo", "body": map[string]any{"kind": "json", "value": map[string]any{"prompt": "test", "seconds": 5, "mode": "pro", "metadata": map[string]any{"mode": mode}}}})
			action, ok := decoded["action"].(string)
			require.True(t, ok)
			input := driverInput("kling-v2-5-turbo", action, decoded["requestBody"])
			out := hookObject(t, p, "buildSubmitRequest", input)
			require.Equal(t, "std", out["body"].(map[string]any)["mode"])
			facts := hookObject(t, p, "extractUsage", input)
			require.Equal(t, 1.5, facts["units"], "effective std mode is five seconds at .3 credit/second")
		}
	}
}
