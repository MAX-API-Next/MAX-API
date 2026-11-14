// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"bytes"
	"encoding/json"
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
	driver := map[string]any{"requestBody": map[string]any{"prompt": "synthetic", "mode": "std", "duration": 5}, "action": "text_to_video", "model": "kling-v2-5-turbo", "upstreamModel": "kling-v2-5-turbo", "baseUrl": provider.URL, "apiKey": "synthetic-only", "upstream": map[string]any{"kind": "vendor"}, "usagePurpose": "facts"}
	request := func(hook string) (json.RawMessage, int) {
		wire, err := p.Call(t.Context(), hook, driver)
		require.NoError(t, err)
		var descriptor struct {
			URL, Method string
			Headers     map[string]string
			Body        json.RawMessage
		}
		require.NoError(t, common.Unmarshal(wire, &descriptor))
		req, err := http.NewRequestWithContext(t.Context(), descriptor.Method, descriptor.URL, bytes.NewReader(descriptor.Body))
		require.NoError(t, err)
		for k, v := range descriptor.Headers {
			req.Header.Set(k, v)
		}
		resp, err := provider.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return body, resp.StatusCode
	}
	body, status := request("buildSubmitRequest")
	wire, err := p.Call(t.Context(), "parseSubmitResponse", driver, map[string]any{"statusCode": status, "body": json.RawMessage(body)})
	require.NoError(t, err)
	var submitted struct {
		TaskID string `json:"taskId"`
	}
	require.NoError(t, common.Unmarshal(wire, &submitted))
	require.Equal(t, "supplier-1", submitted.TaskID)
	wire, err = p.Call(t.Context(), "extractUsage", driver)
	require.NoError(t, err)
	var estimated types.TaskPricingFacts
	require.NoError(t, common.Unmarshal(wire, &estimated))
	require.Equal(t, 1.5, estimated["units"])
	driver["taskId"] = submitted.TaskID
	body, _ = request("buildQueryRequest")
	result, err := p.Call(t.Context(), "parseTaskResult", driver, json.RawMessage(body))
	require.NoError(t, err)
	wire, err = p.Call(t.Context(), "extractUsageOnComplete", driver, json.RawMessage(result), json.RawMessage(body))
	require.NoError(t, err)
	var actual types.TaskPricingFacts
	require.NoError(t, common.Unmarshal(wire, &actual))
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
