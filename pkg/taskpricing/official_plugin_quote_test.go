// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/plugins"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

// This exercises the real JS hooks and raw local-provider HTTP payloads. It
// deliberately does not claim gateway admission, persisted tasks or funding.
func TestOfficialKlingSubmitQueryFactsAndPrice(t *testing.T) {
	submissions, queries := 0, 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/v1/videos/text2video" {
			submissions++
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if common.Unmarshal(raw, &body) != nil || body["model_name"] != "kling-v2-5-turbo" || body["duration"] != "5" {
				t.Error("provider received wrong model or duration")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1"}}`)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/v1/videos/text2video/supplier-1" {
			queries++
			_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1","task_status":"succeed","final_unit_deduction":"0.6"}}`)
			return
		}
		t.Error("unexpected provider operation")
		w.WriteHeader(400)
	}))
	defer provider.Close()
	source, err := plugins.TaskSources.ReadFile("tasks/kling/1.2.0/plugin.js")
	require.NoError(t, err)
	p, err := taskplugin.LoadJavaScript(bytes.NewReader(source), "f4b949679dba31e287d2b21d4b3b4c09980c9bb744da9ede5153f1801cf84240")
	require.NoError(t, err)
	host := taskplugin.NewHost()
	_, err = host.Install(p)
	require.NoError(t, err)
	e, err := host.BindExecutor(taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "sample"}, "kling", "kling-v2-5-turbo", "kling-v2-5-turbo", "text_to_video")
	require.NoError(t, err)
	intent, err := e.DecodeProtocol(t.Context(), "openai_video", "", taskplugin.DecodeInput{Method: "POST", Path: "/v1/videos", Body: []byte(`{"prompt":"synthetic","seconds":5,"mode":"std"}`)})
	require.NoError(t, err)
	channel := taskplugin.ChannelInput{ID: 3, BaseURL: provider.URL, APIKey: "synthetic-only", UpstreamKind: "vendor"}
	b, err := e.NewHTTPBroker(channel, provider.Client(), func(string) error { return nil })
	require.NoError(t, err)
	prepared, err := e.Prepare(t.Context(), channel, intent.RequestBody)
	require.NoError(t, err)
	estimated := prepared.Facts()
	require.Equal(t, 1.5, estimated["units"])
	submitted, err := prepared.Submit(t.Context(), b, func(taskplugin.ExecutionBinding) error { return nil })
	require.NoError(t, err)
	require.Equal(t, "supplier-1", submitted.Task.UpstreamTaskID)
	polled, err := e.Poll(t.Context(), b, channel, submitted.Task, func(taskplugin.ExecutionBinding) error { return nil })
	require.NoError(t, err)
	actual := polled.ActualFacts
	require.Equal(t, 0.6, actual["units"])
	quote, err := PreviewPluginSchema(PreviewInput{Profile: NewAPITask, Source: Source{Driver: "task_plugin", PluginKey: "kling"}, OriginalModel: "kling-v2-5-turbo", MappedModel: "kling-v2-5-turbo", Prices: types.TaskPricingMaps{PluginBillingExpr: map[string]string{"kling::kling-v2-5-turbo": `u("units") * 0.1`}}, Submit: estimated, Actual: actual, PricingAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: 1}, map[string]types.TaskUsageField{"units": {Type: "number", Unit: "credit"}})
	require.NoError(t, err)
	// Independent rational arithmetic: 3/2 * 1/10 * 1000 = 150;
	// 3/5 * 1/10 * 1000 = 60, hence refund difference 90. Never bill the
	// rounded legacy completionTokens (ceil(.6) == 1) as one whole credit.
	require.Equal(t, 150, quote.ReservedQuota)
	require.Equal(t, 60, quote.Final.QuotaAfterGroup)
	require.Equal(t, -90, *quote.FinalMinusReserved)
	require.Equal(t, 1, submissions)
	require.Equal(t, 1, queries)
}

func TestPluginConfigurationPriceIgnoresCompatibleAuxiliaryFacts(t *testing.T) {
	input := PreviewInput{Profile: NewAPITask, Source: Source{Driver: "task_plugin", PluginKey: "hailuo"}, OriginalModel: "MiniMax-Hailuo-2.3", MappedModel: "MiniMax-Hailuo-2.3", Prices: types.TaskPricingMaps{PluginBillingExpr: map[string]string{"hailuo::MiniMax-Hailuo-2.3": `u("seconds") * 0.1`}}, Submit: types.TaskPricingFacts{"seconds": float64(6), "resolution": "768P", "input_images": float64(0), "input_video_seconds": float64(0)}, Actual: types.TaskPricingFacts{"resolution": "1080P"}, PricingAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: 1}
	schema := map[string]types.TaskUsageField{"seconds": {Type: "number", Unit: "second"}, "resolution": {Type: "enum", Enum: []string{"768P", "1080P"}}}
	quote, err := PreviewPluginSchema(input, schema)
	require.NoError(t, err)
	// Independent: 6 s * USD .1/s * 1000 quota/USD = 600. The zero
	// auxiliary dimensions are preserved evidence, not extra multipliers.
	require.Equal(t, 600, quote.ReservedQuota)
	require.Equal(t, 600, quote.Final.QuotaAfterGroup)
	require.Zero(t, *quote.FinalMinusReserved)
	input.Prices.PluginBillingExpr["hailuo::MiniMax-Hailuo-2.3"] = `u("input_images") * 0.1`
	_, err = PreviewPluginSchema(input, schema)
	require.Error(t, err, "retaining auxiliary evidence does not silently expand the selected price schema")
}
