package controller

import (
	"fmt"
	"math"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

func billingQuotaDisplayAmount(quota int64) (float64, error) {
	amount := float64(quota)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return amount, nil
	}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, fmt.Errorf("invalid QuotaPerUnit: %g", common.QuotaPerUnit)
	}
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeCNY {
		if operation_setting.USDExchangeRate <= 0 || math.IsNaN(operation_setting.USDExchangeRate) || math.IsInf(operation_setting.USDExchangeRate, 0) {
			return 0, fmt.Errorf("invalid USDExchangeRate: %g", operation_setting.USDExchangeRate)
		}
		return amount / common.QuotaPerUnit * operation_setting.USDExchangeRate, nil
	}
	return amount / common.QuotaPerUnit, nil
}

func billingSubscriptionDisplayAmount(quota int64, unlimitedToken bool) (float64, error) {
	if unlimitedToken {
		return 100000000, nil
	}
	return billingQuotaDisplayAmount(quota)
}

func writeBillingOpenAIError(c *gin.Context, status int, message string, errorType string) {
	c.JSON(status, gin.H{
		"error": types.OpenAIError{
			Message: message,
			Type:    errorType,
		},
	})
}

func GetSubscription(c *gin.Context) {
	var remainQuota int64
	var usedQuota int64
	var err error
	var token *model.Token
	var expiredTime int64
	if common.DisplayTokenStatEnabled {
		tokenId := c.GetInt("token_id")
		token, err = model.GetTokenById(tokenId)
		if err != nil {
			writeBillingOpenAIError(c, http.StatusOK, err.Error(), "upstream_error")
			return
		}
		expiredTime = token.ExpiredTime
		remainQuota = int64(token.RemainQuota)
		usedQuota = int64(token.UsedQuota)
	} else {
		userId := c.GetInt("id")
		remainQuota, err = model.GetUserQuota(userId, false)
		if err != nil {
			writeBillingOpenAIError(c, http.StatusOK, err.Error(), "upstream_error")
			return
		}
		usedQuota, err = model.GetUserUsedQuota(userId)
		if err != nil {
			writeBillingOpenAIError(c, http.StatusOK, err.Error(), "upstream_error")
			return
		}
	}
	if expiredTime <= 0 {
		expiredTime = 0
	}
	quota := remainQuota + usedQuota
	amount, err := billingSubscriptionDisplayAmount(quota, token != nil && token.UnlimitedQuota)
	if err != nil {
		common.SysError("billing subscription quota display failed: " + err.Error())
		writeBillingOpenAIError(c, http.StatusInternalServerError, err.Error(), "max_api_error")
		return
	}
	subscription := OpenAISubscriptionResponse{
		Object:             "billing_subscription",
		HasPaymentMethod:   true,
		SoftLimitUSD:       amount,
		HardLimitUSD:       amount,
		SystemHardLimitUSD: amount,
		AccessUntil:        expiredTime,
	}
	c.JSON(http.StatusOK, subscription)
	return
}

func GetUsage(c *gin.Context) {
	var quota int64
	var err error
	var token *model.Token
	if common.DisplayTokenStatEnabled {
		tokenId := c.GetInt("token_id")
		token, err = model.GetTokenById(tokenId)
		if err != nil {
			writeBillingOpenAIError(c, http.StatusOK, err.Error(), "max_api_error")
			return
		}
		quota = int64(token.UsedQuota)
	} else {
		userId := c.GetInt("id")
		quota, err = model.GetUserUsedQuota(userId)
		if err != nil {
			writeBillingOpenAIError(c, http.StatusOK, err.Error(), "max_api_error")
			return
		}
	}
	amount, err := billingQuotaDisplayAmount(quota)
	if err != nil {
		common.SysError("billing usage quota display failed: " + err.Error())
		writeBillingOpenAIError(c, http.StatusInternalServerError, err.Error(), "max_api_error")
		return
	}
	usage := OpenAIUsageResponse{
		Object:     "list",
		TotalUsage: amount * 100,
	}
	c.JSON(http.StatusOK, usage)
	return
}
