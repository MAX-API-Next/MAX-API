package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
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
	var request TieredBillingUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request body"})
		return
	}
	if request.Config == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Config must be a JSON object"})
		return
	}
	config := make(map[string]model.LegacyTieredPricingItem, len(request.Config))
	for rawName, item := range request.Config {
		name := strings.TrimSpace(rawName)
		if _, duplicate := config[name]; duplicate {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "Duplicate normalized model name"})
			return
		}
		config[name] = model.LegacyTieredPricingItem{Enabled: item.Enabled, Expr: strings.TrimSpace(item.Expr)}
	}
	if err := model.UpdateLegacyTieredPricing(config); err != nil {
		if errors.Is(err, model.ErrModelPricingLegacyWrite) {
			pricingAPIError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
