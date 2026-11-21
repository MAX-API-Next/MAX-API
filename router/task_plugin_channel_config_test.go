// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package router

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/middleware"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/plugins"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPluginChannelConfigAndPricingLifecycle(t *testing.T) {
	engine, db, root := taskPluginManagementEngine(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Task{}, &model.Option{}, &model.TaskPluginDeletion{}))
	oldPublished, err := common.Marshal(billing_setting.GetPluginBillingExprCopy())
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{billing_setting.PluginBillingExprOption: string(oldPublished)}))
	})
	setting := system_setting.GetFetchSetting()
	oldProtection, oldQuotaUnit := setting.EnableSSRFProtection, common.QuotaPerUnit
	t.Cleanup(func() { setting.EnableSSRFProtection, common.QuotaPerUnit = oldProtection, oldQuotaUnit })
	common.QuotaPerUnit = 1000
	login := func(role string) []*http.Cookie {
		r := httptest.NewRecorder()
		engine.ServeHTTP(r, httptest.NewRequest("GET", "/plugin-test-login/"+role, nil))
		return r.Result().Cookies()
	}
	rootCookies, adminCookies := login("root"), login("admin")
	call := func(method, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
		wire, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(wire))
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		r := httptest.NewRecorder()
		engine.ServeHTTP(r, req)
		return r
	}
	catalog, err := service.CurrentTaskPluginCatalog()
	require.NoError(t, err)
	install := func(version string) *taskplugin.Package {
		source, err := plugins.TaskSources.ReadFile("tasks/kling/" + version + "/plugin.js")
		require.NoError(t, err)
		p, err := taskplugin.LoadUpload(bytes.NewReader(source), "")
		require.NoError(t, err)
		require.NoError(t, catalog.Install(t.Context(), p))
		return p
	}
	first := install("1.2.0")
	r := call("GET", "/api/task_plugins", nil, rootCookies)
	require.Contains(t, r.Body.String(), `"channel_configurable":true`)
	require.Contains(t, r.Body.String(), `"execution_ready":false`)
	require.Contains(t, r.Body.String(), `"admission_ready":false`)
	const credential = "fixture-credential-only"
	const alias = "isolated-kling"
	const upstreamModel = "kling-v2-5-turbo"
	var posts, gets atomic.Int32
	supplier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			t.Error("wrong channel credential")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos/text2video":
			posts.Add(1)
			var payload map[string]any
			if common.DecodeJson(r.Body, &payload) != nil || payload["model_name"] != upstreamModel {
				t.Error("wrong upstream model")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1"}}`)
		case "GET /v1/videos/text2video/supplier-1":
			gets.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1","task_status":"succeed","final_unit_deduction":"0.6"}}`)
		default:
			t.Error("unexpected supplier request")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer supplier.Close()
	channel := map[string]any{
		"type": constant.ChannelTypeTaskPlugin, "name": "isolated-channel", "models": " " + alias + " , " + alias,
		"model_mapping": `{"isolated-kling":"kling-v2-5-turbo"}`, "group": "default", "status": 1,
		"tag":     "review-bulk-tag",
		"setting": `{"task_plugin_key":"kling","task_plugin_upstream":"vendor","task_plugin_config":{}}`,
		"key":     credential, "base_url": supplier.URL, "expected_task_plugin_digest": first.Digest(),
	}
	add := map[string]any{"mode": "single", "channel": channel}
	setting.EnableSSRFProtection = true
	r = call("POST", "/api/channel/", add, rootCookies)
	require.Contains(t, r.Body.String(), "blocked by the fetch policy")
	require.NotContains(t, r.Body.String(), credential)
	setting.EnableSSRFProtection = false
	r = call("POST", "/api/channel/", add, adminCookies)
	require.Equal(t, 403, r.Code)
	channel["model_mapping"] = `{"isolated-kling":"wrong-model"}`
	r = call("POST", "/api/channel/", add, rootCookies)
	require.Contains(t, r.Body.String(), "must be declared")
	channel["model_mapping"] = `{"isolated-kling":"kling-v2-5-turbo"}`
	r = call("POST", "/api/channel/", add, rootCookies)
	require.Contains(t, r.Body.String(), `"success":true`, r.Body.String())
	var stored model.Channel
	require.NoError(t, db.First(&stored, "name = ?", channel["name"]).Error)
	require.Empty(t, stored.TaskPluginExpectedDigest)
	require.NotContains(t, *stored.Setting, first.Digest())
	t.Run("bulk tag edits include plugin channels only for root", func(t *testing.T) {
		ordinary := model.Channel{
			Name: "ordinary-tagged", Type: constant.ChannelTypeOpenAI, Key: "fixture-only",
			Tag: stored.Tag, Models: "ordinary-model", Group: "default",
		}
		require.NoError(t, ordinary.Insert())
		t.Cleanup(func() { require.NoError(t, ordinary.Delete()) })
		const group = "review-group"
		body := map[string]any{"tag": *stored.Tag, "groups": group}
		var pluginRow model.Channel
		response := call("PUT", "/api/channel/tag", body, adminCookies)
		require.Contains(t, response.Body.String(), `"success":true`)
		require.NoError(t, db.First(&ordinary, ordinary.Id).Error)
		require.Equal(t, group, ordinary.Group)
		require.NoError(t, db.First(&pluginRow, stored.Id).Error)
		require.Equal(t, "default", pluginRow.Group)
		response = call("PUT", "/api/channel/tag", body, rootCookies)
		require.Contains(t, response.Body.String(), `"success":true`)
		require.NoError(t, db.First(&pluginRow, stored.Id).Error)
		require.Equal(t, group, pluginRow.Group)
		body["groups"] = "default"
		response = call("PUT", "/api/channel/tag", body, rootCookies)
		require.Contains(t, response.Body.String(), `"success":true`)

		status := func(path string, cookies []*http.Cookie, ordinaryStatus, pluginStatus int) {
			t.Helper()
			response := call("POST", "/api/channel/tag/"+path, map[string]string{"tag": *stored.Tag}, cookies)
			require.Contains(t, response.Body.String(), `"success":true`)
			require.NoError(t, db.First(&ordinary, ordinary.Id).Error)
			require.NoError(t, db.First(&pluginRow, stored.Id).Error)
			require.Equal(t, ordinaryStatus, ordinary.Status)
			require.Equal(t, pluginStatus, pluginRow.Status)
		}
		enabled, disabled := common.ChannelStatusEnabled, common.ChannelStatusManuallyDisabled
		status("disabled", adminCookies, disabled, enabled)
		status("disabled", rootCookies, disabled, disabled)
		status("enabled", adminCookies, enabled, disabled)
		status("enabled", rootCookies, enabled, enabled)
	})
	var count int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", stored.Id).Count(&count).Error)
	require.Zero(t, count, "preview channels cannot enter production routing")
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.ErrorContains(t, middleware.SetupContextForSelectedChannel(ginContext, &stored, alias), "admission is unavailable")
	verifyForcedRouteBlocked(t, &stored)
	channelPath := fmt.Sprintf("/api/channel/%d", stored.Id)
	r = call("GET", channelPath, nil, rootCookies)
	require.Contains(t, r.Body.String(), "task_plugin_key")
	require.NotContains(t, r.Body.String(), credential)
	require.NotContains(t, r.Body.String(), "expected_task_plugin_digest")
	r = call("PUT", "/api/channel/", map[string]any{
		"id": stored.Id, "base_url": supplier.URL + "/new", "expected_task_plugin_digest": first.Digest(),
	}, rootCookies)
	require.Contains(t, r.Body.String(), "Enter a credential")
	r = call("PUT", "/api/channel/", map[string]any{
		"id": stored.Id, "name": "isolated-renamed", "setting": channel["setting"], "expected_task_plugin_digest": first.Digest(),
	}, rootCookies)
	require.Contains(t, r.Body.String(), `"success":true`, r.Body.String())
	require.NotContains(t, r.Body.String(), credential)
	require.NoError(t, db.First(&stored, stored.Id).Error)
	require.Equal(t, "isolated-renamed", stored.Name)
	require.True(t, stored.Key == credential, "omitted credential must be retained")

	t.Run("rejects an update based on a stale channel binding", func(t *testing.T) {
		const callback = "task_plugin_channel:replace_binding_after_read"
		const newAlias = "concurrent-kling"
		const newMapping = `{"concurrent-kling":"kling-v2-5-turbo"}`
		var changed bool
		require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
			channel, ok := tx.Statement.Dest.(*model.Channel)
			if changed || tx.Error != nil || !ok || channel.Id != stored.Id {
				return
			}
			changed = true
			tx.AddError(db.Model(&model.Channel{}).Where("id = ?", stored.Id).
				UpdateColumns(map[string]any{"models": newAlias, "model_mapping": newMapping}).Error)
		}))
		t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove(callback)) })
		response := call("PUT", "/api/channel/", map[string]any{
			"id": stored.Id, "models": alias, "expected_task_plugin_digest": first.Digest(),
		}, rootCookies)
		require.True(t, changed, "the competing write must run after the handler read")
		require.Contains(t, response.Body.String(), "Task plugin channel configuration changed")
		var committed model.Channel
		require.NoError(t, db.First(&committed, stored.Id).Error)
		require.Equal(t, newAlias, committed.Models)
		require.Equal(t, newMapping, *committed.ModelMapping)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", stored.Id).
			UpdateColumns(map[string]any{"models": stored.Models, "model_mapping": stored.ModelMapping}).Error)
	})

	actor := taskplugin.Actor{UserID: root.Id, TokenID: 2, ChannelID: stored.Id, RequestID: "isolated-request"}
	executor, input, err := service.BindConfiguredTaskPlugin(t.Context(), catalog, &stored, actor, alias, "text_to_video")
	require.NoError(t, err)
	require.Equal(t, first.Digest(), executor.Binding().PackageDigest)
	broker, err := service.NewTaskPluginHTTPBroker(executor, input, "")
	require.NoError(t, err)
	prepared, err := executor.Prepare(t.Context(), input, []byte(`{"duration":5,"mode":"std","prompt":"fixture"}`))
	require.NoError(t, err)
	authorize := func(binding taskplugin.ExecutionBinding) error {
		require.Equal(t, executor.Binding(), binding)
		return nil // test-only authorization, never routed through a paid endpoint
	}
	submitted, err := prepared.Submit(t.Context(), broker, authorize)
	require.NoError(t, err)
	wire, err := common.Marshal(submitted.Task)
	require.NoError(t, err)
	require.NotContains(t, string(wire), credential)
	var prior taskplugin.ExecutionTask
	require.NoError(t, common.Unmarshal(wire, &prior))

	second := install("1.2.0-maxapi.1")
	transition := func(action, version string) error {
		_, rev, err := catalog.ManagementSnapshot(t.Context())
		require.NoError(t, err)
		return catalog.MutateExpected(t.Context(), rev, taskplugin.ManagementMutation{Action: action, Key: "kling", Version: version})
	}
	require.NoError(t, transition("activate", second.Manifest().Version))
	r = call("PUT", "/api/channel/", map[string]any{
		"id": stored.Id, "setting": channel["setting"], "expected_task_plugin_digest": first.Digest(),
	}, rootCookies)
	require.Contains(t, r.Body.String(), "options changed")
	next, _, err := service.BindConfiguredTaskPlugin(t.Context(), catalog, &stored, actor, alias, "text_to_video")
	require.NoError(t, err)
	require.Equal(t, second.Digest(), next.Binding().PackageDigest)
	require.NoError(t, transition("disable", second.Manifest().Version))
	_, _, err = service.BindConfiguredTaskPlugin(t.Context(), catalog, &stored, actor, alias, "text_to_video")
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	r = call("PUT", "/api/channel/", map[string]any{
		"id": stored.Id, "setting": channel["setting"], "expected_task_plugin_digest": second.Digest(),
	}, rootCookies)
	require.Contains(t, r.Body.String(), "plugin is unavailable")
	history, err := catalog.Host().RestoreExecutor(prior.Binding, actor)
	require.NoError(t, err)
	polled, err := history.Poll(t.Context(), broker, input, prior, authorize)
	require.NoError(t, err)
	require.Equal(t, 0.6, polled.ActualFacts["units"])
	require.ErrorIs(t, transition("delete", first.Manifest().Version), taskplugin.ErrCatalogInUse)
	require.NoError(t, catalog.Revoke(t.Context(), "kling", first.Manifest().Version))
	_, err = catalog.Host().RestoreExecutor(prior.Binding, actor)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	require.EqualValues(t, 1, posts.Load())
	require.EqualValues(t, 1, gets.Load())

	require.NoError(t, transition("enable", second.Manifest().Version))
	snapshot, err := model.GetModelPricingSnapshot([]string{alias})
	require.NoError(t, err)
	entry := snapshot.Entries[0]
	channelContextID := fmt.Sprintf("channel:%d", stored.Id)
	require.Condition(t, func() bool {
		for _, context := range entry.Contexts {
			if context.ID == channelContextID && context.MappedModel == upstreamModel {
				return true
			}
		}
		return false
	}, "whitespace and duplicates must preserve the normalized alias's channel context")
	require.Contains(t, entry.PluginVariants[0].Schema, "units")
	require.False(t, entry.PluginVariants[0].ExecutionReady)
	expr := `u("units") >= 1 ? tier("tier-1", 0.05 + u("units") * 0.2) : tier("default", u("units") * 0.1)`
	quote := model.ModelPricingQuoteRequest{
		ModelName: alias, ContextID: entry.PluginVariants[0].ID, Group: "default",
		ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion,
		Pricing: map[string]any{"billing_setting.plugin_billing_expr": map[string]any{"kling": expr}},
		Submit:  map[string]any{"units": 1.5}, Actual: map[string]any{"units": 0.6},
	}
	r = call("POST", "/api/option/model_pricing/quote", quote, rootCookies)
	var envelope struct {
		Success bool                    `json:"success"`
		Data    model.ModelPricingQuote `json:"data"`
	}
	require.NoError(t, common.Unmarshal(r.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, r.Body.String())
	require.False(t, envelope.Data.AdmissionReady)
	require.InDelta(t, 0.35, envelope.Data.Preview.Estimate.Amount, 1e-12)
	require.Equal(t, 350, envelope.Data.Preview.Estimate.QuotaAfterGroup)
	require.NotNil(t, envelope.Data.Preview.Final)
	require.InDelta(t, 0.06, envelope.Data.Preview.Final.Amount, 1e-12)
	require.Equal(t, 60, envelope.Data.Preview.Final.QuotaAfterGroup)
	quote.Actual = nil
	r = call("POST", "/api/option/model_pricing/quote", quote, rootCookies)
	require.Contains(t, r.Body.String(), `"final":null`, r.Body.String())
	quote.Actual = map[string]any{"units": 0}
	r = call("POST", "/api/option/model_pricing/quote", quote, rootCookies)
	require.NoError(t, common.Unmarshal(r.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, r.Body.String())
	require.NotNil(t, envelope.Data.Preview.Final)
	require.Zero(t, envelope.Data.Preview.Final.Amount)
	require.NoError(t, db.Model(&model.Task{}).Count(&count).Error)
	require.Zero(t, count, "configuration and quotes create no tasks")
	require.NoError(t, db.Model(&model.Option{}).Count(&count).Error)
	require.Zero(t, count, "draft quote does not persist price options")
	change := map[string]any{
		"model_name": alias, "expected_version": entry.Version, "expected_contract_digest": entry.ContractDigest,
		"expected_binding_version": entry.BindingVersion, "pricing": quote.Pricing,
	}
	patch := map[string]any{"changes": []map[string]any{change}}
	r = call("PATCH", "/api/option/model_pricing", patch, rootCookies)
	require.Contains(t, r.Body.String(), `"committed":true`, r.Body.String())
	snapshot, err = model.GetModelPricingSnapshot([]string{alias})
	require.NoError(t, err)
	require.Equal(t, expr, snapshot.Entries[0].Configured[billing_setting.PluginBillingExprOption].(map[string]any)["kling"])
	r = call("PATCH", "/api/option/model_pricing", patch, rootCookies)
	require.Equal(t, http.StatusConflict, r.Code, r.Body.String())
	token, _, err := billingexpr.RunExpr(`tier("base", p * 2 + c * 5)`, billingexpr.TokenParams{P: 100000, C: 20000})
	require.NoError(t, err)
	require.InDelta(t, 0.3, token/1_000_000, 1e-12)
}

func verifyForcedRouteBlocked(t *testing.T, channel *model.Channel) {
	t.Helper()
	engine := gin.New()
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprint(channel.Id))
	}, middleware.Distribute(), func(c *gin.Context) {
		t.Error("configuration-only channel reached the relay handler")
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(`{"model":"isolated-kling"}`))
	req.Header.Set("Content-Type", "application/json")
	r := httptest.NewRecorder()
	engine.ServeHTTP(r, req)
	require.Equal(t, http.StatusForbidden, r.Code, r.Body.String())
}
