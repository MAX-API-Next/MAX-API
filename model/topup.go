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
	ErrPaymentMethodMismatch   = errors.New("payment method mismatch")
	ErrTopUpNotFound           = errors.New("topup not found")
	ErrTopUpStatusInvalid      = errors.New("topup status invalid")
	ErrInvalidTopUpQuota       = errors.New("invalid top-up quota")
	ErrTopUpQuotaLimitExceeded = errors.New("top-up quota limit exceeded")
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
	if tx == nil || topUp == nil || topUp.Id == 0 {
		return ErrTopUpNotFound
	}
	now := common.GetTimestamp()
	result := tx.Model(&TopUp{}).
		Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
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
		if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
			return err
		}

		if err := creditTopUpQuota(tx, topUp.UserId, int64(creditedQuota), map[string]interface{}{"stripe_customer": customerId}, &cacheTask); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
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

		if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
			return err
		}

		if err := creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil, &cacheTask); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("epay topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("top-up failed, please retry later")
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
	var quotaToAdd int
	var cacheTask CacheInvalidationTask
	var payMoney float64
	var paymentMethod string

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

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		// 计算应充值额度：
		// - Stripe 订单：Money 代表经分组倍率换算后的美元数量，直接 * QuotaPerUnit
		// - 其他订单（如易支付）：Amount 为美元数量，* QuotaPerUnit
		if topUp.PaymentProvider == PaymentProviderStripe {
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			converted, quotaErr := common.QuotaFromDecimalStrict(decimal.NewFromFloat(topUp.Money).Mul(dQuotaPerUnit))
			if quotaErr != nil {
				return ErrInvalidTopUpQuota
			}
			quotaToAdd = converted
		} else {
			dAmount := decimal.NewFromInt(topUp.Amount)
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			converted, quotaErr := common.QuotaFromDecimalStrict(dAmount.Mul(dQuotaPerUnit))
			if quotaErr != nil {
				return ErrInvalidTopUpQuota
			}
			quotaToAdd = converted
		}
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := creditTopUpQuota(tx, topUp.UserId, int64(quotaToAdd), nil, &cacheTask); err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return err
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
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = WithNormalizedEmailWriteTx(customerEmail, func(tx *gorm.DB) error {
		err := withRowLock(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
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
		if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
			return err
		}

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if err := addCreemCustomerEmailUpdateIfAvailable(tx, topUp.UserId, customerEmail, updateFields); err != nil {
			return err
		}

		if err := creditTopUpQuota(tx, topUp.UserId, quota, updateFields, &cacheTask); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
	}
	dispatchStagedCacheInvalidation(cacheTask)

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func addCreemCustomerEmailUpdateIfAvailable(tx *gorm.DB, userId int, customerEmail string, updateFields map[string]interface{}) error {
	customerEmail = NormalizeEmail(customerEmail)
	if customerEmail == "" {
		return nil
	}

	var user User
	if err := tx.Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Email != "" {
		return nil
	}

	if err := ensureEmailAvailableWithTx(tx, customerEmail, user.Id); err != nil {
		if errors.Is(err, ErrEmailAlreadyTaken) {
			return nil
		}
		return err
	}
	updateFields["email"] = customerEmail
	updateFields["normalized_email"] = customerEmail
	return nil
}

func RechargeWaffo(tradeNo string, callerIp string, validations ...PaymentValidation) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	var cacheTask CacheInvalidationTask
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

		if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
			return err
		}

		if err := creditTopUpQuota(tx, topUp.UserId, int64(quotaToAdd), nil, &cacheTask); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
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

		if err := markPendingTopUpSuccessTx(tx, topUp); err != nil {
			return err
		}

		if err := creditTopUpQuota(tx, topUp.UserId, int64(quotaToAdd), nil, &cacheTask); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		if preserved := preservePaymentValidationError(err); preserved != nil {
			return preserved
		}
		return errors.New("充值失败，请稍后重试")
	}
	dispatchStagedCacheInvalidation(cacheTask)

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}

	return nil
}
