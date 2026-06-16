package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

type TieredBillingConfigItem struct {
	Enabled bool   `json:"enabled"`
	Expr    string `json:"expr"`
}

type TieredBillingUpdateRequest struct {
	Config map[string]TieredBillingConfigItem `json:"config"`
}

func parseStringMapOption(raw string) (map[string]string, error) {
	result := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return result, nil
	}
	if err := common.UnmarshalJsonStr(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = make(map[string]string)
	}
	return result, nil
}

func UpdateTieredBillingConfig(c *gin.Context) {
	var req TieredBillingUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	if req.Config == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Config must be a JSON object",
		})
		return
	}

	common.OptionMapRWMutex.RLock()
	rawMode := common.Interface2String(common.OptionMap["billing_setting.billing_mode"])
	rawExpr := common.Interface2String(common.OptionMap["billing_setting.billing_expr"])
	common.OptionMapRWMutex.RUnlock()

	modeMap, err := parseStringMapOption(rawMode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Existing billing mode config is invalid: " + err.Error(),
		})
		return
	}
	exprMap, err := parseStringMapOption(rawExpr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Existing billing expression config is invalid: " + err.Error(),
		})
		return
	}

	for modelName, mode := range modeMap {
		if mode == billing_setting.BillingModeTieredExpr {
			delete(modeMap, modelName)
			delete(exprMap, modelName)
		}
	}

	for modelName, item := range req.Config {
		name := strings.TrimSpace(modelName)
		if name == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Model name cannot be empty",
			})
			return
		}

		delete(modeMap, modelName)
		delete(exprMap, modelName)
		if name != modelName {
			delete(modeMap, name)
			delete(exprMap, name)
		}

		if !item.Enabled {
			continue
		}

		expr := strings.TrimSpace(item.Expr)
		if expr == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("Billing expression for %s cannot be empty", name),
			})
			return
		}
		if err := billing_setting.SmokeTestExpr(expr); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("Billing expression for %s is invalid: %v", name, err),
			})
			return
		}

		modeMap[name] = billing_setting.BillingModeTieredExpr
		exprMap[name] = expr
	}

	modeBytes, err := common.Marshal(modeMap)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	exprBytes, err := common.Marshal(exprMap)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.UpdateOptionsBulk(map[string]string{
		"billing_setting.billing_mode": string(modeBytes),
		"billing_setting.billing_expr": string(exprBytes),
	}); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
