package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type tokenResponse struct {
	*model.Token
	Routing       *model.TokenRoutingPolicy `json:"routing,omitempty"`
	RoutingLegacy bool                      `json:"routing_legacy,omitempty"`
}

func buildMaskedTokenResponse(token *model.Token, userGroup string) *tokenResponse {
	if token == nil {
		return nil
	}
	maskedToken := *token
	maskedToken.Key = token.GetMaskedKey()
	routing, err := token.GetStoredRoutingPolicy()
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to decode token routing policy (tokenId=%d): %s", token.Id, err.Error()))
	}
	routingLegacy := routing == nil && err == nil
	if routingLegacy {
		legacy := service.LegacyTokenRoutingPolicy(token.Group, token.CrossGroupRetry, userGroup)
		routing = &legacy
	}
	return &tokenResponse{Token: &maskedToken, Routing: routing, RoutingLegacy: routingLegacy}
}

func buildMaskedTokenResponses(tokens []*model.Token, userGroup string) []*tokenResponse {
	maskedTokens := make([]*tokenResponse, 0, len(tokens))
	for _, token := range tokens {
		maskedTokens = append(maskedTokens, buildMaskedTokenResponse(token, userGroup))
	}
	return maskedTokens
}

type tokenMutationRequest struct {
	Id                 int             `json:"id"`
	Status             int             `json:"status"`
	Name               string          `json:"name"`
	ExpiredTime        int64           `json:"expired_time"`
	RemainQuota        int64           `json:"remain_quota"`
	UnlimitedQuota     bool            `json:"unlimited_quota"`
	ModelLimitsEnabled bool            `json:"model_limits_enabled"`
	ModelLimits        string          `json:"model_limits"`
	AllowIps           *string         `json:"allow_ips"`
	Group              *string         `json:"group"`
	CrossGroupRetry    bool            `json:"cross_group_retry"`
	Routing            json.RawMessage `json:"routing"`
}

func requestUserGroup(c *gin.Context) string {
	userGroup := c.GetString("group")
	if userGroup == "" {
		userGroup = c.GetString("user_group")
	}
	return userGroup
}

func validateAssignableTokenGroup(c *gin.Context, group string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return true
	}
	if service.CanUseTokenGroupRuntime(requestUserGroup(c), group) {
		return true
	}
	common.ApiErrorI18n(c, i18n.MsgTokenGroupNotAssignable, map[string]any{"Group": group})
	return false
}

func resolveTokenMutationRouting(request tokenMutationRequest, current *model.Token, userGroup string, creating bool) (model.TokenRoutingPolicy, error) {
	if len(request.Routing) > 0 {
		if strings.TrimSpace(string(request.Routing)) == "null" {
			return service.NormalizeLegacyTokenRoutingPolicy(service.DefaultTokenRoutingPolicy(), userGroup)
		}
		var policy model.TokenRoutingPolicy
		if err := common.Unmarshal(request.Routing, &policy); err != nil {
			return model.TokenRoutingPolicy{}, fmt.Errorf("invalid token routing policy: %w", err)
		}
		return service.NormalizeTokenRoutingPolicy(policy, userGroup)
	}

	if request.Group != nil {
		return service.NormalizeLegacyTokenRoutingPolicy(
			service.LegacyTokenRoutingPolicy(*request.Group, request.CrossGroupRetry, userGroup),
			userGroup,
		)
	}
	if creating {
		return service.NormalizeLegacyTokenRoutingPolicy(service.DefaultTokenRoutingPolicy(), userGroup)
	}
	if current == nil {
		return model.TokenRoutingPolicy{}, errors.New("current token is required")
	}
	stored, err := current.GetStoredRoutingPolicy()
	if err != nil {
		return model.TokenRoutingPolicy{}, fmt.Errorf("invalid token routing policy: %w", err)
	}
	if stored != nil {
		return stored.Clone(), nil
	}
	return service.NormalizeLegacyTokenRoutingPolicy(
		service.LegacyTokenRoutingPolicy(current.Group, current.CrossGroupRetry, userGroup),
		userGroup,
	)
}

func tokenMutationUpdatesRouting(request tokenMutationRequest) bool {
	return len(request.Routing) > 0 || request.Group != nil
}

func GetAllTokens(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	tokens, err := model.GetAllUserTokens(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	total, _ := model.CountUserTokens(userId)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens, requestUserGroup(c)))
	common.ApiSuccess(c, pageInfo)
}

func SearchTokens(c *gin.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := common.GetPageQuery(c)

	tokens, total, err := model.SearchUserTokens(userId, keyword, token, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens, requestUserGroup(c)))
	common.ApiSuccess(c, pageInfo)
}

func GetToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildMaskedTokenResponse(token, requestUserGroup(c)))
}

func GetTokenKey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"key": token.GetFullKey(),
	})
}

func GetTokenStatus(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := model.GetTokenByIds(tokenId, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"object":          "credit_summary",
		"total_granted":   token.RemainQuota,
		"total_used":      0, // not supported currently
		"total_available": token.RemainQuota,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := model.GetTokenByIds(tokenId, userId)
	if err != nil {
		common.SysError("failed to get token by id: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": gin.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        token.RemainQuota + token.UsedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"expires_at":           expiredAt,
		},
	})
}

func AddToken(c *gin.Context) {
	request := tokenMutationRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(request.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if request.Group != nil && !validateAssignableTokenGroup(c, *request.Group) {
		return
	}
	routing, err := resolveTokenMutationRouting(request, nil, requestUserGroup(c), true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	projectedGroup, crossGroupRetry := service.ProjectTokenRoutingPolicy(routing)
	// 非无限额度时，检查额度值是否超出有效范围
	if !request.UnlimitedQuota {
		if request.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := int64(1000000000 * common.QuotaPerUnit)
		if request.RemainQuota > maxQuotaValue {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	// 检查用户令牌数量是否已达上限
	maxTokens := operation_setting.GetMaxUserTokens()
	count, err := model.CountUserTokens(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if int(count) >= maxTokens {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("已达到最大令牌数量限制 (%d)", maxTokens),
		})
		return
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		common.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cleanToken := model.Token{
		UserId:             c.GetInt("id"),
		Name:               request.Name,
		Key:                key,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        request.ExpiredTime,
		RemainQuota:        request.RemainQuota,
		UnlimitedQuota:     request.UnlimitedQuota,
		ModelLimitsEnabled: request.ModelLimitsEnabled,
		ModelLimits:        request.ModelLimits,
		AllowIps:           request.AllowIps,
		Group:              projectedGroup,
		CrossGroupRetry:    crossGroupRetry,
	}
	if err := cleanToken.SetRoutingPolicy(&routing); err != nil {
		common.ApiError(c, err)
		return
	}
	err = cleanToken.Insert()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	err := model.DeleteTokenById(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateToken(c *gin.Context) {
	userId := c.GetInt("id")
	statusOnly := c.Query("status_only")
	request := tokenMutationRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Full updates omit status with its zero value. Status-only updates must persist a valid status.
	if (statusOnly != "" || request.Status != 0) && !isValidTokenStatus(request.Status) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(request.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !request.UnlimitedQuota {
		if request.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := int64(1000000000 * common.QuotaPerUnit)
		if request.RemainQuota > maxQuotaValue {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	cleanToken, err := model.GetTokenByIds(request.Id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Status == common.TokenStatusEnabled {
		expiredTime := cleanToken.ExpiredTime
		remainQuota := cleanToken.RemainQuota
		unlimitedQuota := cleanToken.UnlimitedQuota
		if statusOnly == "" {
			expiredTime = request.ExpiredTime
			remainQuota = request.RemainQuota
			unlimitedQuota = request.UnlimitedQuota
		}
		if cleanToken.Status == common.TokenStatusExpired && expiredTime <= common.GetTimestamp() && expiredTime != -1 {
			common.ApiErrorI18n(c, i18n.MsgTokenExpiredCannotEnable)
			return
		}
		if cleanToken.Status == common.TokenStatusExhausted && remainQuota <= 0 && !unlimitedQuota {
			common.ApiErrorI18n(c, i18n.MsgTokenExhaustedCannotEable)
			return
		}
	}
	if statusOnly != "" {
		cleanToken.Status = request.Status
	} else {
		if request.Group != nil && !validateAssignableTokenGroup(c, *request.Group) {
			return
		}
		// If you add more fields, please also update token.Update()
		cleanToken.Name = request.Name
		cleanToken.ExpiredTime = request.ExpiredTime
		cleanToken.RemainQuota = request.RemainQuota
		cleanToken.UnlimitedQuota = request.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = request.ModelLimitsEnabled
		cleanToken.ModelLimits = request.ModelLimits
		cleanToken.AllowIps = request.AllowIps
		if tokenMutationUpdatesRouting(request) {
			routing, routingErr := resolveTokenMutationRouting(request, cleanToken, requestUserGroup(c), false)
			if routingErr != nil {
				common.ApiError(c, routingErr)
				return
			}
			projectedGroup, crossGroupRetry := service.ProjectTokenRoutingPolicy(routing)
			cleanToken.Group = projectedGroup
			cleanToken.CrossGroupRetry = crossGroupRetry
			if err := cleanToken.SetRoutingPolicy(&routing); err != nil {
				common.ApiError(c, err)
				return
			}
		}
		if request.Status != 0 {
			cleanToken.Status = request.Status
		}
	}
	err = cleanToken.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    buildMaskedTokenResponse(cleanToken, requestUserGroup(c)),
	})
}

func isValidTokenStatus(status int) bool {
	switch status {
	case common.TokenStatusEnabled, common.TokenStatusDisabled, common.TokenStatusExpired, common.TokenStatusExhausted:
		return true
	default:
		return false
	}
}

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func DeleteTokenBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	count, err := model.BatchDeleteTokens(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func GetTokenKeysBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(tokenBatch.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": 100})
		return
	}
	userId := c.GetInt("id")
	tokens, err := model.GetTokenKeysByIds(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	keysMap := make(map[int]string)
	for _, t := range tokens {
		keysMap[t.Id] = t.GetFullKey()
	}
	common.ApiSuccess(c, gin.H{"keys": keysMap})
}
