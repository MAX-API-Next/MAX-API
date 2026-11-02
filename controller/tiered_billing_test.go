package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTieredBillingControllerTestDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	t.Cleanup(func() { model.DB = previousDB })
	preserveModelPricingControllerCaches(t)
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Channel{}))
	model.DB = db

	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{
		"billing_setting.billing_mode": `{"old-tiered":"tiered_expr","remove-me":"tiered_expr","ratio-model":"ratio"}`,
		"billing_setting.billing_expr": `{"old-tiered":"tier(\"old\", p * 1)","remove-me":"tier(\"remove\", p * 1)","ratio-model":"legacy"}`,
	}
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, model.UpdateOptionsBulk(map[string]string{
		"billing_setting.billing_mode": `{"old-tiered":"tiered_expr","remove-me":"tiered_expr","ratio-model":"ratio"}`,
		"billing_setting.billing_expr": `{"old-tiered":"tier(\"old\", p * 1)","remove-me":"tier(\"remove\", p * 1)","ratio-model":"legacy"}`,
	}))

	t.Cleanup(func() {
		_ = model.UpdateOptionsBulk(map[string]string{
			"billing_setting.billing_mode": "{}",
			"billing_setting.billing_expr": "{}",
		})
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func performTieredBillingUpdate(t *testing.T, payload any) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/tiered_billing", bytes.NewReader(body))

	UpdateTieredBillingConfig(ctx)
	return recorder
}

func TestUpdateTieredBillingConfigAtomicallyUpdatesModeAndExpr(t *testing.T) {
	setupTieredBillingControllerTestDB(t)

	recorder := performTieredBillingUpdate(t, gin.H{
		"config": gin.H{
			"new-tiered": gin.H{
				"enabled": true,
				"expr":    `tier("base", p * 2 + c * 8)`,
			},
			"remove-me": gin.H{
				"enabled": false,
				"expr":    `tier("remove", p * 1)`,
			},
		},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	modeMap := billing_setting.GetBillingModeCopy()
	exprMap := billing_setting.GetBillingExprCopy()
	require.NotContains(t, modeMap, "old-tiered")
	require.Equal(t, "tiered_expr", modeMap["new-tiered"])
	require.Equal(t, "ratio", modeMap["ratio-model"])
	require.NotContains(t, modeMap, "remove-me")
	require.Equal(t, `tier("base", p * 2 + c * 8)`, exprMap["new-tiered"])
	require.Equal(t, "legacy", exprMap["ratio-model"])
	require.NotContains(t, exprMap, "old-tiered")
	require.NotContains(t, exprMap, "remove-me")
}

func TestUpdateTieredBillingConfigRejectsInvalidExpression(t *testing.T) {
	setupTieredBillingControllerTestDB(t)

	recorder := performTieredBillingUpdate(t, gin.H{
		"config": gin.H{
			"broken-model": gin.H{
				"enabled": true,
				"expr":    `tier("broken", p *)`,
			},
		},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Contains(t, response.Message, "broken-model")
	require.NotContains(t, billing_setting.GetBillingModeCopy(), "broken-model")
}

func TestUpdateTieredBillingOptionsPreservesCompletePair(t *testing.T) {
	setupTieredBillingControllerTestDB(t)
	options := map[string]string{
		"billing_setting.billing_mode": `{"new-tiered":"tiered_expr","ratio-model":"ratio"}`,
		"billing_setting.billing_expr": `{"new-tiered":"tier(\"base\", p * 2 + c * 8)","ratio-model":"legacy"}`,
	}
	recorder := performTieredBillingUpdate(t, gin.H{"options": options})
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	require.Equal(t, map[string]string{"new-tiered": "tiered_expr", "ratio-model": "ratio"}, billing_setting.GetBillingModeCopy())
	require.Equal(t, map[string]string{"new-tiered": `tier("base", p * 2 + c * 8)`, "ratio-model": "legacy"}, billing_setting.GetBillingExprCopy())
	for key, expected := range options {
		var stored model.Option
		require.NoError(t, model.DB.Where("key = ?", key).First(&stored).Error)
		require.JSONEq(t, expected, stored.Value)
	}
}

func TestUpdateTieredBillingOptionsRejectsIncompleteOrInvalidPair(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload gin.H
		task    bool
		status  int
	}{
		{"partial", gin.H{"options": map[string]string{"billing_setting.billing_mode": "{}"}}, false, 200},
		{"extra", gin.H{"options": map[string]string{"billing_setting.billing_mode": "{}", "billing_setting.billing_expr": "{}", "SystemName": "must-not-write"}}, false, 200},
		{"mixed", gin.H{"options": map[string]string{"billing_setting.billing_mode": "{}", "billing_setting.billing_expr": "{}"}, "config": gin.H{}}, false, 200},
		{"invalid-active-expression", gin.H{"options": map[string]string{"billing_setting.billing_mode": `{"broken":"tiered_expr"}`, "billing_setting.billing_expr": `{"broken":"p *"}`}}, false, 200},
		{"task-price-requires-versioned-api", gin.H{"options": map[string]string{"billing_setting.billing_mode": `{"kling-v1":"tiered_expr"}`, "billing_setting.billing_expr": `{"kling-v1":"u(\"units\") * 0.1"}`}}, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTieredBillingControllerTestDB(t)
			if tc.task {
				require.NoError(t, model.DB.Create(&model.Channel{Type: constant.ChannelTypeKling, Status: 1, Models: "kling-v1", Group: "default", Key: "synthetic"}).Error)
			}
			var before []model.Option
			require.NoError(t, model.DB.Order("key").Find(&before).Error)
			beforeModes, beforeExprs := billing_setting.GetBillingModeCopy(), billing_setting.GetBillingExprCopy()
			recorder := performTieredBillingUpdate(t, tc.payload)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.False(t, response.Success)
			var after []model.Option
			require.NoError(t, model.DB.Order("key").Find(&after).Error)
			require.Equal(t, before, after)
			require.Equal(t, beforeModes, billing_setting.GetBillingModeCopy())
			require.Equal(t, beforeExprs, billing_setting.GetBillingExprCopy())
		})
	}
}
