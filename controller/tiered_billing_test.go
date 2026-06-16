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
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTieredBillingControllerTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
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
