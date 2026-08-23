package model

import (
	"errors"
	"fmt"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch    = errors.New("payment method mismatch")
	ErrTopUpNotFound            = errors.New("topup not found")
	ErrTopUpStatusInvalid       = errors.New("topup status invalid")
	ErrInvalidTopUpQuota        = errors.New("invalid top-up quota")
	ErrTopUpQuotaLimitExceeded  = errors.New("top-up quota limit exceeded")
	ErrTopUpNeedsReconciliation = errors.New("paid top-up requires manual reconciliation")
)

func topUpQuotaMaxCurrent(creditedQuota int64) (int64, error) {
	if creditedQuota <= 0 || creditedQuota >= int64(common.MaxQuota) {
		return 0, ErrInvalidTopUpQuota
	}
	return int64(common.MaxQuota) - 1 - creditedQuota, nil
}

// ValidateTopUpQuotaCapacity is the pre-payment guard. Settlement repeats the
// same invariant in creditTopUpQuota with a single conditional UPDATE.
func ValidateTopUpQuotaCapacity(userId int, creditedQuota int64) error {
	maxCurrent, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}
	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota > maxCurrent {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

// creditTopUpQuota atomically enforces the wallet ceiling. The predicate and
// increment must stay in the same UPDATE so concurrent callbacks cannot both
// pass a separate read/check.
func creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int64, updates map[string]interface{}, cacheTask *CacheInvalidationTask) error {
	maxCurrent, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}
	fields := make(map[string]interface{}, len(updates)+1)
	for key, value := range updates {
		fields[key] = value
	}
	fields["quota"] = gorm.Expr("quota + ?", creditedQuota)
	result := tx.Model(&User{}).Where("id = ? AND quota <= ?", userId, maxCurrent).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		task, err := stageUserCacheInvalidationTx(tx, userId, false)
		if err != nil {
			return err
		}
		if cacheTask != nil {
			*cacheTask = task
		}
		return nil
	}
	var count int64
	if err := tx.Model(&User{}).Where("id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrTopUpQuotaLimitExceeded
}

func preservePaymentValidationError(err error) error {
	if errors.Is(err, ErrPaymentAmountMismatch) || errors.Is(err, ErrPaymentCurrencyMismatch) || errors.Is(err, ErrPaymentMethodMismatch) {
		return err
	}
	return nil
}

func updatePendingTopUpStatusTx(tx *gorm.DB, topUp *TopUp, targetStatus string) error {
	if tx == nil || topUp == nil || topUp.Id == 0 {
		return ErrTopUpNotFound
	}
	result := tx.Model(&TopUp{}).
		Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
		Update("status", targetStatus)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTopUpStatusInvalid
	}
	topUp.Status = targetStatus
	return nil
}

func markPendingTopUpSuccessTx(tx *gorm.DB, topUp *TopUp) error {
	return markTopUpSuccessFromStatusTx(tx, topUp, common.TopUpStatusPending)
}

func markTopUpSuccessFromStatusTx(tx *gorm.DB, topUp *TopUp, expectedStatus string) error {
	if tx == nil || topUp == nil || topUp.Id == 0 {
		return ErrTopUpNotFound
	}
	now := common.GetTimestamp()
	result := tx.Model(&TopUp{}).
		Where("id = ? AND status = ?", topUp.Id, expectedStatus).
		Updates(map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTopUpStatusInvalid
	}
	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = now
	return nil
}

func markPendingTopUpPaidReconciliationTx(tx *gorm.DB, topUp *TopUp) error {
	if tx == nil || topUp == nil || topUp.Id == 0 {
		return ErrTopUpNotFound
	}
	now := common.GetTimestamp()
	result := tx.Model(&TopUp{}).
		Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
		Updates(map[string]interface{}{
			"status":        common.TopUpStatusPaidReconciliation,
			"complete_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTopUpStatusInvalid
	}
	topUp.Status = common.TopUpStatusPaidReconciliation
	topUp.CompleteTime = now
	return nil
}

// settlePaidTopUpTx records exactly one durable result for an externally paid
// order: either quota is credited and the order succeeds, or quota remains
// untouched and the order enters manual reconciliation. Other errors roll the
// transaction back so the provider can retry safely.
func settlePaidTopUpTx(tx *gorm.DB, topUp *TopUp, creditedQuota int64, updates map[string]interface{}, cacheTask *CacheInvalidationTask) (bool, error) {
	if err := creditTopUpQuota(tx, topUp.UserId, creditedQuota, updates, cacheTask); err != nil {
		if !errors.Is(err, ErrTopUpQuotaLimitExceeded) {
			return false, err
		}
		if err := markPendingTopUpPaidReconciliationTx(tx, topUp); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
		return false, err
	}
	return false, nil
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	topUp, _ := GetTopUpByTradeNoWithError(tradeNo)
	return topUp
}

// GetTopUpByTradeNoWithError keeps database failures distinct from a missing
// order for payment callbacks, which must retry when the database is unhealthy.
func GetTopUpByTradeNoWithError(tradeNo string) (*TopUp, error) {
	topUp := &TopUp{}
	if err := DB.Where("trade_no = ?", tradeNo).First(topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTopUpNotFound
		}
		return nil, err
	}
	return topUp, nil
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		return updatePendingTopUpStatusTx(tx, topUp, targetStatus)
	})
}

func Recharge(referenceId string, customerId string, callerIp string, validations ...PaymentValidation) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota float64
	var cacheTask CacheInvalidationTask
	var needsReconciliation bool
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := withRowLock(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status == common.TopUpStatusPaidReconciliation {
			needsReconciliation = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		if err := validatePaymentAgainstTopUp(topUp, validations); err != nil {
			return err
		}

		creditedQuota, quotaErr := common.QuotaFromDecimalStrict(
			decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if quotaErr != nil || creditedQuota <= 0 {
			return errors.New("无效的充值额度")
		}
		quota = float64(creditedQuota)
		needsReconciliation, err = settlePaidTopUpTx(tx, topUp, int64(creditedQuota), map[string]interface{}{"stripe_customer": customerId}, &cacheTask)
		return err
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
	}
	if needsReconciliation {
		return ErrTopUpNeedsReconciliation
	}
	if quota == 0 {
		return nil
	}
	dispatchStagedCacheInvalidation(cacheTask)

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(int(quota)), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string, validations ...PaymentValidation) (err error) {
	if tradeNo == "" {
		return errors.New("missing payment order")
	}

	var quotaToAdd int64
	var cacheTask CacheInvalidationTask
	var needsReconciliation bool
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status == common.TopUpStatusPaidReconciliation {
			needsReconciliation = true
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		if err := validatePaymentAgainstTopUp(topUp, validations); err != nil {
			return err
		}

		convertedQuota, quotaErr := common.QuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
		if quotaErr != nil || convertedQuota <= 0 {
			return errors.New("invalid top-up quota")
		}
		quotaToAdd = int64(convertedQuota)

		if actualPaymentMethod != "" && topUp.PaymentMethod != actualPaymentMethod {
			result := tx.Model(&TopUp{}).
				Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
				Update("payment_method", actualPaymentMethod)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTopUpStatusInvalid
			}
			topUp.PaymentMethod = actualPaymentMethod
		}

		needsReconciliation, err = settlePaidTopUpTx(tx, topUp, quotaToAdd, nil, &cacheTask)
		return err
	})

	if err != nil {
		common.SysError("epay topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("top-up failed, please retry later")
	}
	if needsReconciliation {
		return ErrTopUpNeedsReconciliation
	}
	dispatchStagedCacheInvalidation(cacheTask)

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值额度: %v，支付金额：%.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentProviderEpay)
	}

	return nil
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int64
	var cacheTask CacheInvalidationTask
	var payMoney float64
	var paymentMethod string
	var completed bool

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusPaidReconciliation {
			return errors.New("订单状态不支持补单")
		}
		expectedStatus := topUp.Status

		// 计算应充值额度：
		// - Stripe 订单：Money 代表经分组倍率换算后的美元数量，直接 * QuotaPerUnit
		// - Creem 订单：Amount 已经是最终额度
		// - 其他订单（如易支付）：Amount 为美元数量，* QuotaPerUnit
		switch topUp.PaymentProvider {
		case PaymentProviderStripe:
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			converted, quotaErr := common.QuotaFromDecimalStrict(decimal.NewFromFloat(topUp.Money).Mul(dQuotaPerUnit))
			if quotaErr != nil {
				return ErrInvalidTopUpQuota
			}
			quotaToAdd = int64(converted)
		case PaymentProviderCreem:
			quotaToAdd = topUp.Amount
		default:
			dAmount := decimal.NewFromInt(topUp.Amount)
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			converted, quotaErr := common.QuotaFromDecimalStrict(dAmount.Mul(dQuotaPerUnit))
			if quotaErr != nil {
				return ErrInvalidTopUpQuota
			}
			quotaToAdd = int64(converted)
		}
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil, &cacheTask); err != nil {
			return err
		}
		if err := markTopUpSuccessFromStatusTx(tx, topUp, expectedStatus); err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		completed = true
		return nil
	})

	if err != nil {
		return err
	}
	if !completed {
		return nil
	}
	dispatchStagedCacheInvalidation(cacheTask)

	// 事务外记录日志，避免阻塞
	RecordTopupLog(userId, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quotaToAdd), payMoney), callerIp, paymentMethod, "admin")
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string, validations ...PaymentValidation) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota int64
	var cacheTask CacheInvalidationTask
	var needsReconciliation bool
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := withRowLock(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status == common.TopUpStatusPaidReconciliation {
			needsReconciliation = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		if err := validatePaymentAgainstTopUp(topUp, validations); err != nil {
			return err
		}

		quota = topUp.Amount
		if quota <= 0 {
			return errors.New("无效的充值额度")
		}
		needsReconciliation, err = settlePaidTopUpTx(tx, topUp, quota, nil, &cacheTask)
		return err
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
	}
	if needsReconciliation {
		bestEffortClaimCreemCustomerEmail(topUp.UserId, customerEmail, referenceId)
		return ErrTopUpNeedsReconciliation
	}
	if quota == 0 {
		return nil
	}
	dispatchStagedCacheInvalidation(cacheTask)
	bestEffortClaimCreemCustomerEmail(topUp.UserId, customerEmail, referenceId)

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func bestEffortClaimCreemCustomerEmail(userId int, customerEmail string, referenceId string) {
	if err := claimCreemCustomerEmailIfEmpty(userId, customerEmail); err != nil {
		common.SysError(fmt.Sprintf("creem email claim failed: trade_no=%s user_id=%d error=%s", referenceId, userId, err.Error()))
	}
}

func claimCreemCustomerEmailIfEmpty(userId int, customerEmail string) error {
	customerEmail = NormalizeEmail(customerEmail)
	if customerEmail == "" {
		return nil
	}
	var cacheTask CacheInvalidationTask
	err := WithNormalizedEmailWriteTx(customerEmail, func(tx *gorm.DB) error {
		if err := ensureEmailAvailableWithTx(tx, customerEmail, userId); err != nil {
			if errors.Is(err, ErrEmailAlreadyTaken) {
				return nil
			}
			return err
		}
		result := tx.Model(&User{}).
			Where("id = ? AND (email = ? OR email IS NULL) AND (normalized_email = ? OR normalized_email IS NULL)", userId, "", "").
			Updates(map[string]interface{}{
				"email":            customerEmail,
				"normalized_email": customerEmail,
			})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		task, err := stageUserCacheInvalidationTx(tx, userId, false)
		if err != nil {
			return err
		}
		cacheTask = task
		return nil
	})
	if err != nil {
		return err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return nil
}

func RechargeWaffo(tradeNo string, callerIp string, validations ...PaymentValidation) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	var cacheTask CacheInvalidationTask
	var needsReconciliation bool
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}
		if topUp.Status == common.TopUpStatusPaidReconciliation {
			needsReconciliation = true
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		if err := validatePaymentAgainstTopUp(topUp, validations); err != nil {
			return err
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		convertedQuota, quotaErr := common.QuotaFromDecimalStrict(dAmount.Mul(dQuotaPerUnit))
		if quotaErr != nil || convertedQuota <= 0 {
			return errors.New("无效的充值额度")
		}
		quotaToAdd = convertedQuota

		needsReconciliation, err = settlePaidTopUpTx(tx, topUp, int64(quotaToAdd), nil, &cacheTask)
		return err
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
	}
	if needsReconciliation {
		return ErrTopUpNeedsReconciliation
	}
	dispatchStagedCacheInvalidation(cacheTask)

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string, validations ...PaymentValidation) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	var cacheTask CacheInvalidationTask
	var needsReconciliation bool
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status == common.TopUpStatusPaidReconciliation {
			needsReconciliation = true
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		if err := validatePaymentAgainstTopUp(topUp, validations); err != nil {
			return err
		}

		convertedQuota, quotaErr := common.QuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
		if quotaErr != nil || convertedQuota <= 0 {
			return errors.New("无效的充值额度")
		}
		quotaToAdd = convertedQuota

		needsReconciliation, err = settlePaidTopUpTx(tx, topUp, int64(quotaToAdd), nil, &cacheTask)
		return err
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
	}
	if needsReconciliation {
		return ErrTopUpNeedsReconciliation
	}
	dispatchStagedCacheInvalidation(cacheTask)

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}

	return nil
}
