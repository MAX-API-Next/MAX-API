// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupModelPricingControllerTest(t *testing.T) *gin.Engine {
	t.Helper()
	preserveModelPricingControllerCaches(t)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldQPU := common.QuotaPerUnit
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.QuotaPerUnit = oldQPU
		common.RedisEnabled = oldRedis
	})
	setupTieredBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Log{}))
	model.LOG_DB = model.DB
	common.QuotaPerUnit = 1000
	engine := gin.New()
	engine.GET("/pricing", GetModelPricingConfig)
	engine.PATCH("/pricing", UpdateModelPricingConfig)
	engine.POST("/preview", PreviewModelPricing)
	engine.POST("/quote", QuoteModelPricing)
	engine.PUT("/option", UpdateOption)
	return engine
}
func pricingControllerRequest(t *testing.T, engine *gin.Engine, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := common.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest(method, target, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}
func pricingControllerEntry(t *testing.T, engine *gin.Engine, name string) model.ModelPricingEntry {
	t.Helper()
	response := pricingControllerRequest(t, engine, http.MethodGet, "/pricing?model="+name, nil)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Success bool                       `json:"success"`
		Data    model.ModelPricingSnapshot `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, response.Body.String())
	require.Len(t, envelope.Data.Entries, 1)
	return envelope.Data.Entries[0]
}
func controllerPricingChange(entry model.ModelPricingEntry, draft types.ModelPricingValues) types.ModelPricingChange {
	return types.ModelPricingChange{ModelName: entry.ModelName, Pricing: draft, ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion}
}
func TestModelPricingHTTPPatchConflictAndStrictInput(t *testing.T) {
	engine := setupModelPricingControllerTest(t)
	entry := pricingControllerEntry(t, engine, "sample-http")
	payload := map[string]any{"changes": []types.ModelPricingChange{controllerPricingChange(entry, types.ModelPricingValues{"ModelPrice": float64(0)})}}
	response := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", payload)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"committed":true`)
	response = pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", payload)
	require.Equal(t, http.StatusConflict, response.Code)
	after := pricingControllerEntry(t, engine, "sample-http")
	require.Equal(t, float64(0), after.Configured["ModelPrice"])
	for _, invalid := range []any{
		map[string]any{"changes": []any{map[string]any{"model_name": "sample-http", "expected_version": after.Version, "pricing": nil}}},
		map[string]any{"changes": []any{map[string]any{"model_name": "sample-http", "expected_version": after.Version, "pricing": map[string]any{}, "reset": nil}}},
		map[string]any{"changes": payload["changes"], "ignored": true},
	} {
		response = pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", invalid)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
}
func TestModelPricingHTTPPreviewQuoteAndLegacyGuard(t *testing.T) {
	engine := setupModelPricingControllerTest(t)
	channel := model.Channel{Type: constant.ChannelTypeKling, Status: 1, Models: "kling-v1", Name: "synthetic", Key: "synthetic-only", Group: "default"}
	require.NoError(t, model.DB.Create(&channel).Error)
	entry := pricingControllerEntry(t, engine, "kling-v1")
	draft := types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}
	var before []model.Option
	require.NoError(t, model.DB.Order("key").Find(&before).Error)
	response := pricingControllerRequest(t, engine, http.MethodPost, "/preview", map[string]any{"model_name": "kling-v1", "pricing": draft})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var previewEnvelope struct {
		Data model.ModelPricingEntry `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &previewEnvelope))
	require.Equal(t, entry.Version, previewEnvelope.Data.Version)
	require.NotEqual(t, entry.Version, previewEnvelope.Data.DraftVersion)
	quote := map[string]any{"model_name": "kling-v1", "context_id": fmt.Sprintf("channel:%d", channel.Id), "expected_version": entry.Version, "expected_contract_digest": entry.ContractDigest, "expected_binding_version": entry.BindingVersion, "pricing": draft, "group": "default", "pricing_at": "2026-10-30T00:00:00Z", "submit": map[string]any{"units": 1}, "actual": map[string]any{"units": 0.6}}
	response = pricingControllerRequest(t, engine, http.MethodPost, "/quote", quote)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"admission_ready":false`)
	var envelope struct {
		Data model.ModelPricingQuote `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 60, envelope.Data.Preview.Final.QuotaAfterGroup)
	require.Equal(t, 500, envelope.Data.Preview.ReservedQuota)
	var after []model.Option
	require.NoError(t, model.DB.Order("key").Find(&after).Error)
	require.Equal(t, before, after)
	quote["quota_per_unit"] = 1
	response = pricingControllerRequest(t, engine, http.MethodPost, "/quote", quote)
	require.Equal(t, http.StatusBadRequest, response.Code)
	delete(quote, "quota_per_unit")
	response = pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", map[string]any{"changes": []types.ModelPricingChange{controllerPricingChange(entry, draft)}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = pricingControllerRequest(t, engine, http.MethodPut, "/option", map[string]any{"key": "billing_setting.billing_expr", "value": `{"kling-v1":"0"}`})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	quote["expected_version"] = entry.Version
	response = pricingControllerRequest(t, engine, http.MethodPost, "/quote", quote)
	require.Equal(t, http.StatusConflict, response.Code)
}
