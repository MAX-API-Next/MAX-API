package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func buildTaskConsumptionLogData(c *gin.Context, info *relaycommon.RelayInfo) (string, map[string]interface{}) {
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if info.TaskBilling != nil {
		logContent = fmt.Sprintf("%s，参数化计费 %s/%s: %.4f x %.2f%s", logContent, info.TaskBilling.RuleKey, info.TaskBilling.RowID, info.TaskBilling.UnitPrice, info.TaskBilling.Quantity, info.TaskBilling.Unit)
	} else if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	if c != nil && c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.TaskBilling != nil {
		other["task_billing"] = info.TaskBilling
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	attachQuotaSaturation(c, info, other)
	return logContent, other
}

// BuildTaskSubmissionSettlementEffect creates the durable non-financial effect
// for a successfully submitted task. The enclosing settlement operation keeps
// the log, usage counters, and dashboard update replayable after a restart.
func BuildTaskSubmissionSettlementEffect(c *gin.Context, info *relaycommon.RelayInfo, quota int) *model.BillingSettlementEffect {
	if info == nil {
		return nil
	}
	logContent, other := buildTaskConsumptionLogData(c, info)
	return &model.BillingSettlementEffect{
		LogType:       model.LogTypeConsume,
		Content:       logContent,
		ChannelID:     info.ChannelId,
		ModelName:     info.OriginModelName,
		TokenID:       info.TokenId,
		Group:         info.UsingGroup,
		Other:         other,
		NodeName:      common.NodeName,
		UpdateUsage:   true,
		Quota:         int64(quota),
		QuotaIsActual: true,
	}
}

// LogTaskConsumption records task usage for legacy/custom funding paths that
// do not have a durable BillingSettlement operation to carry the effect.
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := ""
	if c != nil {
		tokenName = c.GetString("token_name")
	}
	logContent, other := buildTaskConsumptionLogData(c, info)
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
		if bc.TaskBilling != nil {
			other["task_billing"] = bc.TaskBilling
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// taskFinalSettlementPending reports only the funding state of the submission
// finalize operation. A pending settlement effect must not block provider
// polling because the debit/refund itself is already durably applied.
func taskFinalSettlementPending(task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.BillingRequestId == "" {
		return false, nil
	}
	status, found, err := model.GetBillingSettlementStatus("request:" + task.PrivateData.BillingRequestId + ":finalize")
	if err != nil || !found {
		return false, err
	}
	switch status {
	case "", model.BillingSettlementStatusApplied:
		return false, nil
	case model.BillingSettlementStatusPending, model.BillingSettlementStatusManual:
		return true, nil
	default:
		return true, fmt.Errorf("unknown task submission settlement status %q", status)
	}
}

// taskTerminalSettlementPending gates the submission-settlement lookup on a
// provider transition that would make the task terminal. Callers keep their
// own error reporting and skip/return behavior.
func taskTerminalSettlementPending(task *model.Task, providerTerminal bool) (bool, error) {
	if !providerTerminal {
		return false, nil
	}
	return taskFinalSettlementPending(task)
}

func BuildTaskRefundSettlementInput(task *model.Task, reason string) *model.BillingSettlementInput {
	if task == nil || task.Quota == 0 || task.ID <= 0 {
		return nil
	}
	reason = common.SanitizePersistedLogContent(reason)
	source := model.BillingSettlementSourceWallet
	if taskIsSubscription(task) {
		source = model.BillingSettlementSourceSubscription
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	return &model.BillingSettlementInput{
		OperationKey:                    fmt.Sprintf("task:%d:refund", task.ID),
		Source:                          source,
		UserID:                          task.UserId,
		SubscriptionID:                  task.PrivateData.SubscriptionId,
		TokenID:                         task.PrivateData.TokenId,
		FundingDelta:                    -int64(task.Quota),
		TokenDelta:                      -int64(task.Quota),
		TaskID:                          task.ID,
		TaskQuota:                       int64(task.Quota),
		TaskQuotaTarget:                 0,
		SubscriptionPreConsumeRequestID: task.PrivateData.BillingRequestId,
		FinalizeSubscriptionPreConsume:  source == model.BillingSettlementSourceSubscription,
		AllowMissingToken:               true,
		Effect: &model.BillingSettlementEffect{
			LogType: model.LogTypeRefund, Content: reason, ChannelID: task.ChannelId,
			ModelName: taskModelName(task), TokenID: task.PrivateData.TokenId,
			Group: task.Group, Other: other, NodeName: task.PrivateData.NodeName,
		},
	}
}

func buildTaskFinalSettlementInput(task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) *model.BillingSettlementInput {
	if task == nil || task.ID <= 0 || actualQuota <= 0 || actualQuota == task.Quota {
		return nil
	}
	reason = common.SanitizePersistedLogContent(reason)
	quotaDelta := actualQuota - task.Quota
	source := model.BillingSettlementSourceWallet
	if taskIsSubscription(task) {
		source = model.BillingSettlementSourceSubscription
	}
	tokenDelta := int64(quotaDelta)
	if task.PrivateData.TokenId <= 0 {
		tokenDelta = 0
	}
	logType := model.LogTypeRefund
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = task.Quota
	other["actual_quota"] = actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	return &model.BillingSettlementInput{
		OperationKey:                    fmt.Sprintf("task:%d:finalize", task.ID),
		Source:                          source,
		UserID:                          task.UserId,
		SubscriptionID:                  task.PrivateData.SubscriptionId,
		TokenID:                         task.PrivateData.TokenId,
		FundingDelta:                    int64(quotaDelta),
		TokenDelta:                      tokenDelta,
		TaskID:                          task.ID,
		TaskQuota:                       int64(task.Quota),
		TaskQuotaTarget:                 int64(actualQuota),
		SubscriptionPreConsumeRequestID: task.PrivateData.BillingRequestId,
		AllowMissingToken:               quotaDelta < 0,
		Effect: &model.BillingSettlementEffect{
			LogType: logType, Content: reason, ChannelID: task.ChannelId,
			ModelName: taskModelName(task), TokenID: task.PrivateData.TokenId,
			Group: task.Group, Other: other, NodeName: task.PrivateData.NodeName,
			UpdateUsage: quotaDelta > 0,
		},
	}
}

func applyTaskBillingSettlement(ctx context.Context, task *model.Task, input *model.BillingSettlementInput) bool {
	if input == nil {
		return true
	}
	appliedFundingDelta, _, err := model.ApplyBillingSettlementOnce(*input)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("任务结算仍待恢复 task %s operation=%s: %s", task.TaskID, input.OperationKey, err.Error()))
		return false
	}
	task.Quota = int(input.TaskQuota + appliedFundingDelta)
	if effectErr := model.ProcessBillingSettlementEffect(input.OperationKey); effectErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("任务结算日志/统计仍待恢复 task %s: %s", task.TaskID, effectErr.Error()))
	}
	if appliedFundingDelta != input.FundingDelta {
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 结算资金实际应用=%d，目标=%d，账面额度=%d", task.TaskID, appliedFundingDelta, input.FundingDelta, task.Quota))
	}
	return true
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	if task == nil {
		return false
	}
	if task.Quota == 0 {
		return true
	}
	if task.ID <= 0 {
		logger.LogWarn(ctx, fmt.Sprintf("拒绝退款未持久化任务 task %s", task.TaskID))
		return false
	}

	return applyTaskBillingSettlement(ctx, task, BuildTaskRefundSettlementInput(task, reason))
}

func ApplyTaskBillingSettlement(ctx context.Context, task *model.Task, input *model.BillingSettlementInput) bool {
	return applyTaskBillingSettlement(ctx, task, input)
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) {
	if actualQuota <= 0 {
		return
	}
	if task == nil || task.ID <= 0 {
		logger.LogWarn(ctx, "拒绝对未持久化任务执行差额结算")
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	applyTaskBillingSettlement(ctx, task, buildTaskFinalSettlementInput(task, actualQuota, reason, clamps...))
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	actualQuota, reason, clamp, ok := calculateTaskQuotaByTokens(task, totalTokens)
	if !ok {
		return
	}
	RecalculateTaskQuota(ctx, task, actualQuota, reason, clamp)
}

func calculateTaskQuotaByTokens(task *model.Task, totalTokens int) (int, string, *common.QuotaClamp, bool) {
	if task == nil || totalTokens <= 0 {
		return 0, "", nil, false
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return 0, "", nil, false
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return 0, "", nil, false
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	return actualQuota, reason, clamp, true
}
