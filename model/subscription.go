package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/cachex"
	"github.com/samber/hot"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Subscription duration units
const (
	SubscriptionDurationYear   = "year"
	SubscriptionDurationMonth  = "month"
	SubscriptionDurationDay    = "day"
	SubscriptionDurationHour   = "hour"
	SubscriptionDurationCustom = "custom"
)

// Subscription quota reset period
const (
	SubscriptionResetNever   = "never"
	SubscriptionResetDaily   = "daily"
	SubscriptionResetWeekly  = "weekly"
	SubscriptionResetMonthly = "monthly"
	SubscriptionResetCustom  = "custom"
)

var (
	ErrSubscriptionOrderNotFound      = errors.New("subscription order not found")
	ErrSubscriptionOrderStatusInvalid = errors.New("subscription order status invalid")
	ErrSubscriptionQuotaInsufficient  = errors.New("subscription quota insufficient")
)

func invalidateSubscriptionUserCache(userId int, operation string) {
	if userId <= 0 {
		return
	}
	if cacheErr := invalidateUserCache(userId); cacheErr != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate user cache after %s (user_id=%d): %v", operation, userId, cacheErr))
		enqueueUserCacheInvalidationRetry(userId, cacheErr)
	}
}

func completePendingSubscriptionOrderTx(tx *gorm.DB, order *SubscriptionOrder, providerPayload string, actualPaymentMethod string) (bool, error) {
	if tx == nil || order == nil || order.Id == 0 {
		return false, ErrSubscriptionOrderNotFound
	}
	now := common.GetTimestamp()
	updates := map[string]interface{}{
		"status":        common.TopUpStatusSuccess,
		"complete_time": now,
	}
	if providerPayload != "" {
		updates["provider_payload"] = providerPayload
		order.ProviderPayload = providerPayload
	}
	if actualPaymentMethod != "" && order.PaymentMethod != actualPaymentMethod {
		updates["payment_method"] = actualPaymentMethod
		order.PaymentMethod = actualPaymentMethod
	}
	result := tx.Model(&SubscriptionOrder{}).
		Where("id = ? AND status = ?", order.Id, common.TopUpStatusPending).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		var latest SubscriptionOrder
		if err := tx.Select("status").Where("id = ?", order.Id).First(&latest).Error; err == nil && latest.Status == common.TopUpStatusSuccess {
			return false, nil
		}
		return false, ErrSubscriptionOrderStatusInvalid
	}
	order.Status = common.TopUpStatusSuccess
	order.CompleteTime = now
	return true, nil
}

func expirePendingSubscriptionOrderTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if tx == nil || order == nil || order.Id == 0 {
		return ErrSubscriptionOrderNotFound
	}
	now := common.GetTimestamp()
	result := tx.Model(&SubscriptionOrder{}).
		Where("id = ? AND status = ?", order.Id, common.TopUpStatusPending).
		Updates(map[string]interface{}{
			"status":        common.TopUpStatusExpired,
			"complete_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		var latest SubscriptionOrder
		if err := tx.Select("status").Where("id = ?", order.Id).First(&latest).Error; err == nil && latest.Status != common.TopUpStatusPending {
			return nil
		}
		return ErrSubscriptionOrderStatusInvalid
	}
	order.Status = common.TopUpStatusExpired
	order.CompleteTime = now
	return nil
}

const (
	subscriptionPlanCacheNamespace     = "max-api:subscription_plan:v1"
	subscriptionPlanInfoCacheNamespace = "max-api:subscription_plan_info:v1"
)

var (
	subscriptionPlanCacheOnce     sync.Once
	subscriptionPlanInfoCacheOnce sync.Once

	subscriptionPlanCache     *cachex.HybridCache[SubscriptionPlan]
	subscriptionPlanInfoCache *cachex.HybridCache[SubscriptionPlanInfo]
)

func subscriptionPlanCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_TTL", 300)
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanInfoCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_TTL", 120)
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_CAP", 5000)
	if capacity <= 0 {
		capacity = 5000
	}
	return capacity
}

func subscriptionPlanInfoCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_CAP", 10000)
	if capacity <= 0 {
		capacity = 10000
	}
	return capacity
}

func getSubscriptionPlanCache() *cachex.HybridCache[SubscriptionPlan] {
	subscriptionPlanCacheOnce.Do(func() {
		ttl := subscriptionPlanCacheTTL()
		subscriptionPlanCache = cachex.NewHybridCache[SubscriptionPlan](cachex.HybridCacheConfig[SubscriptionPlan]{
			Namespace: cachex.Namespace(subscriptionPlanCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlan]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlan] {
				return hot.NewHotCache[string, SubscriptionPlan](hot.LRU, subscriptionPlanCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanCache
}

func getSubscriptionPlanInfoCache() *cachex.HybridCache[SubscriptionPlanInfo] {
	subscriptionPlanInfoCacheOnce.Do(func() {
		ttl := subscriptionPlanInfoCacheTTL()
		subscriptionPlanInfoCache = cachex.NewHybridCache[SubscriptionPlanInfo](cachex.HybridCacheConfig[SubscriptionPlanInfo]{
			Namespace: cachex.Namespace(subscriptionPlanInfoCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlanInfo]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlanInfo] {
				return hot.NewHotCache[string, SubscriptionPlanInfo](hot.LRU, subscriptionPlanInfoCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanInfoCache
}

func subscriptionPlanCacheKey(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func InvalidateSubscriptionPlanCache(planId int) {
	if planId <= 0 {
		return
	}
	cache := getSubscriptionPlanCache()
	_, _ = cache.DeleteMany([]string{subscriptionPlanCacheKey(planId)})
	infoCache := getSubscriptionPlanInfoCache()
	_ = infoCache.Purge()
}

// Subscription plan
type SubscriptionPlan struct {
	Id int `json:"id"`

	Title    string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64 `json:"price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	AllowBalancePay *bool `json:"allow_balance_pay" gorm:"default:true"`

	StripePriceId         string `json:"stripe_price_id" gorm:"type:varchar(128);default:''"`
	CreemProductId        string `json:"creem_product_id" gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId string `json:"waffo_pancake_product_id" gorm:"type:varchar(128);default:''"`

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int `json:"max_purchase_per_user" gorm:"type:int;default:0"`

	// Upgrade user group after purchase (empty = no change)
	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64 `json:"total_amount" gorm:"type:bigint;not null;default:0"`

	// Quota reset period for plan
	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (p *SubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *SubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (p *SubscriptionPlan) NormalizeDefaults() {
	if p.AllowBalancePay == nil {
		p.AllowBalancePay = common.GetPointer(true)
	}
}

// Subscription order (payment -> webhook -> create UserSubscription)
type SubscriptionOrder struct {
	Id     int     `json:"id"`
	UserId int     `json:"user_id" gorm:"index"`
	PlanId int     `json:"plan_id" gorm:"index"`
	Money  float64 `json:"money"`

	TradeNo         string `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	Status          string `json:"status"`
	CreateTime      int64  `json:"create_time"`
	CompleteTime    int64  `json:"complete_time"`

	ProviderPayload string `json:"provider_payload" gorm:"type:text"`
}

func (o *SubscriptionOrder) Insert() error {
	if o.CreateTime == 0 {
		o.CreateTime = common.GetTimestamp()
	}
	return DB.Create(o).Error
}

func (o *SubscriptionOrder) Update() error {
	return DB.Save(o).Error
}

func GetSubscriptionOrderByTradeNo(tradeNo string) *SubscriptionOrder {
	if tradeNo == "" {
		return nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return nil
	}
	return &order
}

// User subscription instance
type UserSubscription struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index;index:idx_user_sub_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_user_sub_active,priority:3"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_user_sub_active,priority:2"` // active/expired/cancelled

	Source string `json:"source" gorm:"type:varchar(32);default:'order'"` // order/admin

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	UpgradeGroup  string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *UserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *UserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

type SubscriptionSummary struct {
	Subscription *UserSubscription `json:"subscription"`
}

type SubscriptionResetResult struct {
	PlanId           int    `json:"plan_id"`
	MatchedCount     int    `json:"matched_count"`
	ResetCount       int    `json:"reset_count"`
	UserCount        int    `json:"user_count"`
	AdvanceResetTime bool   `json:"advance_reset_time"`
	PlanTitle        string `json:"-"`
	AffectedUserIds  []int  `json:"-"`
}

func calcPlanEndTime(start time.Time, plan *SubscriptionPlan) (int64, error) {
	if plan == nil {
		return 0, errors.New("plan is nil")
	}
	if plan.DurationValue <= 0 && plan.DurationUnit != SubscriptionDurationCustom {
		return 0, errors.New("duration_value must be > 0")
	}
	switch plan.DurationUnit {
	case SubscriptionDurationYear:
		return start.AddDate(plan.DurationValue, 0, 0).Unix(), nil
	case SubscriptionDurationMonth:
		return start.AddDate(0, plan.DurationValue, 0).Unix(), nil
	case SubscriptionDurationDay:
		return start.Add(time.Duration(plan.DurationValue) * 24 * time.Hour).Unix(), nil
	case SubscriptionDurationHour:
		return start.Add(time.Duration(plan.DurationValue) * time.Hour).Unix(), nil
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 {
			return 0, errors.New("custom_seconds must be > 0")
		}
		return start.Add(time.Duration(plan.CustomSeconds) * time.Second).Unix(), nil
	default:
		return 0, fmt.Errorf("invalid duration_unit: %s", plan.DurationUnit)
	}
}

func NormalizeResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly, SubscriptionResetCustom:
		return strings.TrimSpace(period)
	default:
		return SubscriptionResetNever
	}
}

func calcNextResetTime(base time.Time, plan *SubscriptionPlan, endUnix int64) int64 {
	if plan == nil {
		return 0
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetNever {
		return 0
	}
	var next time.Time
	switch period {
	case SubscriptionResetDaily:
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, 1)
	case SubscriptionResetWeekly:
		// Align to next Monday 00:00
		weekday := int(base.Weekday()) // Sunday=0
		// Convert to Monday=1..Sunday=7
		if weekday == 0 {
			weekday = 7
		}
		daysUntil := 8 - weekday
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, daysUntil)
	case SubscriptionResetMonthly:
		// Align to first day of next month 00:00
		next = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).
			AddDate(0, 1, 0)
	case SubscriptionResetCustom:
		if plan.QuotaResetCustomSeconds <= 0 {
			return 0
		}
		next = base.Add(time.Duration(plan.QuotaResetCustomSeconds) * time.Second)
	default:
		return 0
	}
	if endUnix > 0 && next.Unix() > endUnix {
		return 0
	}
	return next.Unix()
}

func GetSubscriptionPlanById(id int) (*SubscriptionPlan, error) {
	return getSubscriptionPlanByIdTx(nil, id)
}

func getSubscriptionPlanByIdTx(tx *gorm.DB, id int) (*SubscriptionPlan, error) {
	if id <= 0 {
		return nil, errors.New("invalid plan id")
	}
	key := subscriptionPlanCacheKey(id)
	if key != "" {
		if cached, found, err := getSubscriptionPlanCache().Get(key); err == nil && found {
			cached.NormalizeDefaults()
			return &cached, nil
		}
	}
	var plan SubscriptionPlan
	query := DB
	if tx != nil {
		query = tx
	}
	if err := query.Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	plan.NormalizeDefaults()
	_ = getSubscriptionPlanCache().SetWithTTL(key, plan, subscriptionPlanCacheTTL())
	return &plan, nil
}

func CountUserSubscriptionsByPlan(userId int, planId int) (int64, error) {
	if userId <= 0 || planId <= 0 {
		return 0, errors.New("invalid userId or planId")
	}
	var count int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", userId, planId).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func getUserGroupByIdTx(tx *gorm.DB, userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid userId")
	}
	if tx == nil {
		tx = DB
	}
	var group string
	if err := tx.Model(&User{}).Where("id = ?", userId).Select(commonGroupCol).Find(&group).Error; err != nil {
		return "", err
	}
	return group, nil
}

func downgradeUserGroupForSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid downgrade args")
	}
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	if upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
	if err != nil {
		return "", err
	}
	if currentGroup != upgradeGroup {
		return "", nil
	}
	activeQuery := tx.Where("user_id = ? AND status = ? AND end_time > ? AND id <> ? AND upgrade_group <> ''",
		sub.UserId, "active", now, sub.Id).
		Where("TRIM(upgrade_group) = ?", currentGroup).
		Limit(1).
		Find(&UserSubscription{})
	if activeQuery.Error != nil {
		return "", activeQuery.Error
	}
	if activeQuery.RowsAffected > 0 {
		return "", nil
	}
	prevGroup := strings.TrimSpace(sub.PrevUserGroup)
	if prevGroup == "" || prevGroup == currentGroup {
		return "", nil
	}
	if err := tx.Model(&User{}).Where("id = ?", sub.UserId).
		Update("group", prevGroup).Error; err != nil {
		return "", err
	}
	return prevGroup, nil
}

func CreateUserSubscriptionFromPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, source string) (*UserSubscription, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	if plan == nil || plan.Id == 0 {
		return nil, errors.New("invalid plan")
	}
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	if plan.MaxPurchasePerUser > 0 {
		var lockedUser User
		if err := withRowLock(tx).Select("id").Where("id = ?", userId).First(&lockedUser).Error; err != nil {
			return nil, err
		}

		var count int64
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND plan_id = ?", userId, plan.Id).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return nil, errors.New("已达到该套餐购买上限")
		}
	}
	nowUnix := getDBTimestampTx(tx)
	now := time.Unix(nowUnix, 0)
	endUnix, err := calcPlanEndTime(now, plan)
	if err != nil {
		return nil, err
	}
	resetBase := now
	nextReset := calcNextResetTime(resetBase, plan, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = now.Unix()
	}
	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	prevGroup := ""
	if upgradeGroup != "" {
		currentGroup, err := getUserGroupByIdTx(tx, userId)
		if err != nil {
			return nil, err
		}
		if currentGroup != upgradeGroup {
			prevGroup = currentGroup
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", upgradeGroup).Error; err != nil {
				return nil, err
			}
		}
	}
	sub := &UserSubscription{
		UserId:        userId,
		PlanId:        plan.Id,
		AmountTotal:   plan.TotalAmount,
		AmountUsed:    0,
		StartTime:     now.Unix(),
		EndTime:       endUnix,
		Status:        "active",
		Source:        source,
		LastResetTime: lastReset,
		NextResetTime: nextReset,
		UpgradeGroup:  upgradeGroup,
		PrevUserGroup: prevGroup,
		CreatedAt:     common.GetTimestamp(),
		UpdatedAt:     common.GetTimestamp(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

// Complete a subscription order (idempotent). Creates a UserSubscription snapshot from the plan.
// expectedPaymentProvider guards against cross-gateway callback attacks (empty skips the check).
// actualPaymentMethod updates the order's PaymentMethod to reflect the real payment type used (empty skips update).
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string, validations ...PaymentValidation) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	var logUserId int
	var logPlanTitle string
	var logMoney float64
	var logPaymentMethod string
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionOrderNotFound
			}
			return err
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		plan, err := getSubscriptionPlanByIdTx(tx, order.PlanId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			// still allow completion for already purchased orders
		}
		if err := validatePaymentAgainstSubscriptionOrder(&order, plan, validations); err != nil {
			return err
		}
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		claimed, err := completePendingSubscriptionOrderTx(tx, &order, providerPayload, actualPaymentMethod)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		_, err = CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		logUserId = order.UserId
		logPlanTitle = plan.Title
		logMoney = order.Money
		logPaymentMethod = order.PaymentMethod
		return nil
	})
	if err != nil {
		return err
	}
	if upgradeGroup != "" && logUserId > 0 {
		invalidateSubscriptionUserCache(logUserId, "subscription order completion")
	}
	if logUserId > 0 {
		msg := fmt.Sprintf("订阅购买成功，套餐: %s，支付金额: %.2f，支付方式: %s", logPlanTitle, logMoney, logPaymentMethod)
		RecordLog(logUserId, LogTypeTopup, msg)
	}
	return nil
}

func upsertSubscriptionTopUpTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}
	now := common.GetTimestamp()
	var topup TopUp
	if err := tx.Where("trade_no = ?", order.TradeNo).First(&topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = TopUp{
				UserId:        order.UserId,
				Amount:        0,
				Money:         order.Money,
				TradeNo:       order.TradeNo,
				PaymentMethod: order.PaymentMethod,
				CreateTime:    order.CreateTime,
				CompleteTime:  now,
				Status:        common.TopUpStatusSuccess,
			}
			return tx.Create(&topup).Error
		}
		return err
	}
	topup.Money = order.Money
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return ErrPaymentMethodMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = common.TopUpStatusSuccess
	return tx.Save(&topup).Error
}

func ExpireSubscriptionOrder(tradeNo string, expectedPaymentProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := withRowLock(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return nil
		}
		return expirePendingSubscriptionOrderTx(tx, &order)
	})
}

// Admin bind (no payment). Creates a UserSubscription from a plan.
func AdminBindSubscription(userId int, planId int, sourceNote string) (string, error) {
	if userId <= 0 || planId <= 0 {
		return "", errors.New("invalid userId or planId")
	}
	plan, err := GetSubscriptionPlanById(planId)
	if err != nil {
		return "", err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		_, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "admin")
		return err
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(plan.UpgradeGroup) != "" {
		invalidateSubscriptionUserCache(userId, "admin subscription binding")
		return fmt.Sprintf("用户分组将升级到 %s", plan.UpgradeGroup), nil
	}
	return "", nil
}

func calcSubscriptionBalanceQuota(priceAmount float64) (int, error) {
	if priceAmount <= 0 {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("额度单位配置错误")
	}
	quota := decimal.NewFromFloat(priceAmount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Ceil().
		IntPart()
	return int(quota), nil
}

// PurchaseSubscriptionWithBalance creates a subscription by deducting the user's wallet quota.
func PurchaseSubscriptionWithBalance(userId int, planId int) error {
	if userId <= 0 || planId <= 0 {
		return errors.New("invalid userId or planId")
	}

	var logPlanTitle string
	var logMoney float64
	var chargedQuota int
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("套餐未启用")
		}
		if plan.PriceAmount < 0 {
			return errors.New("套餐价格不能为负数")
		}
		if plan.AllowBalancePay != nil && !*plan.AllowBalancePay {
			return errors.New("该套餐不允许使用余额兑换")
		}

		requiredQuota, err := calcSubscriptionBalanceQuota(plan.PriceAmount)
		if err != nil {
			return err
		}

		var user User
		if err := withRowLock(tx).Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if requiredQuota > 0 && user.Quota < int64(requiredQuota) {
			return errors.New("余额不足")
		}
		if requiredQuota > 0 {
			result := tx.Model(&User{}).Where("id = ? AND quota >= ?", userId, requiredQuota).
				Update("quota", gorm.Expr("quota - ?", requiredQuota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("余额不足")
			}
		}

		if _, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, PaymentMethodBalance); err != nil {
			return err
		}

		now := common.GetTimestamp()
		tradeNo := fmt.Sprintf("SUBBALUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
		order := &SubscriptionOrder{
			UserId:          userId,
			PlanId:          plan.Id,
			Money:           plan.PriceAmount,
			TradeNo:         tradeNo,
			PaymentMethod:   PaymentMethodBalance,
			PaymentProvider: PaymentProviderBalance,
			Status:          common.TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			ProviderPayload: fmt.Sprintf("charged_quota=%d", requiredQuota),
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		logPlanTitle = plan.Title
		logMoney = plan.PriceAmount
		chargedQuota = requiredQuota
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		return nil
	})
	if err != nil {
		return err
	}

	if chargedQuota > 0 || upgradeGroup != "" {
		invalidateSubscriptionUserCache(userId, "subscription purchase")
	}
	msg := fmt.Sprintf("使用余额购买订阅成功，套餐: %s，支付金额: %.2f，扣除额度: %d", logPlanTitle, logMoney, chargedQuota)
	RecordLog(userId, LogTypeTopup, msg)
	return nil
}

// GetAllActiveUserSubscriptions returns all active subscriptions for a user.
func GetAllActiveUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var subs []UserSubscription
	err := DB.Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

// HasActiveUserSubscription returns whether the user has any active subscription.
// This is a lightweight existence check to avoid heavy pre-consume transactions.
func HasActiveUserSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var count int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetAllUserSubscriptions returns all subscriptions (active and expired) for a user.
func GetAllUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	var subs []UserSubscription
	err := DB.Where("user_id = ?", userId).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

func buildSubscriptionSummaries(subs []UserSubscription) []SubscriptionSummary {
	if len(subs) == 0 {
		return []SubscriptionSummary{}
	}
	result := make([]SubscriptionSummary, 0, len(subs))
	for _, sub := range subs {
		subCopy := sub
		result = append(result, SubscriptionSummary{
			Subscription: &subCopy,
		})
	}
	return result
}

// AdminInvalidateUserSubscription marks a user subscription as cancelled and ends it immediately.
func AdminInvalidateUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := withRowLock(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"status":     "cancelled",
			"end_time":   now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		invalidateSubscriptionUserCache(userId, "subscription cancellation")
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := withRowLock(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		if err := tx.Where("id = ?", userSubscriptionId).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		invalidateSubscriptionUserCache(userId, "subscription deletion")
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

func resetUserSubscriptionTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64, advanceResetTime bool) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	sub.AmountUsed = 0
	if advanceResetTime {
		nextReset := calcNextResetTime(time.Unix(now, 0), plan, sub.EndTime)
		sub.NextResetTime = nextReset
		if nextReset > 0 {
			sub.LastResetTime = now
		} else {
			sub.LastResetTime = 0
		}
	}
	return tx.Save(sub).Error
}

func buildSubscriptionResetResult(plan *SubscriptionPlan, subs []UserSubscription, advanceResetTime bool) *SubscriptionResetResult {
	userIds := make([]int, 0, len(subs))
	seenUsers := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if _, ok := seenUsers[sub.UserId]; ok {
			continue
		}
		seenUsers[sub.UserId] = struct{}{}
		userIds = append(userIds, sub.UserId)
	}
	return &SubscriptionResetResult{
		PlanId:           plan.Id,
		MatchedCount:     len(subs),
		ResetCount:       len(subs),
		UserCount:        len(userIds),
		AdvanceResetTime: advanceResetTime,
		PlanTitle:        plan.Title,
		AffectedUserIds:  userIds,
	}
}

func adminResetUserSubscriptionsByPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, now int64, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if tx == nil || plan == nil {
		return nil, errors.New("invalid reset args")
	}
	var subs []UserSubscription
	if err := withRowLock(tx).
		Where("user_id = ? AND plan_id = ? AND status = ? AND end_time > ?", userId, plan.Id, "active", now).
		Order("end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, errors.New("该用户没有有效的此套餐订阅")
	}
	for i := range subs {
		if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, advanceResetTime); err != nil {
			return nil, err
		}
	}
	return buildSubscriptionResetResult(plan, subs, advanceResetTime), nil
}

func adminResetPlanSubscriptionsTx(tx *gorm.DB, plan *SubscriptionPlan, now int64, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if tx == nil || plan == nil {
		return nil, errors.New("invalid reset args")
	}
	var subs []UserSubscription
	if err := withRowLock(tx).
		Where("plan_id = ? AND status = ? AND end_time > ?", plan.Id, "active", now).
		Order("user_id asc, end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	for i := range subs {
		if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, advanceResetTime); err != nil {
			return nil, err
		}
	}
	return buildSubscriptionResetResult(plan, subs, advanceResetTime), nil
}

func AdminResetUserSubscriptionsByPlan(userId int, planId int, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if userId <= 0 || planId <= 0 {
		return nil, errors.New("invalid userId or planId")
	}
	var result *SubscriptionResetResult
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		result, err = adminResetUserSubscriptionsByPlanTx(tx, userId, plan, now, advanceResetTime)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func AdminResetPlanSubscriptions(planId int, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if planId <= 0 {
		return nil, errors.New("invalid planId")
	}
	var result *SubscriptionResetResult
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		result, err = adminResetPlanSubscriptionsTx(tx, plan, now, advanceResetTime)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type SubscriptionPreConsumeResult struct {
	UserSubscriptionId int
	PreConsumed        int64
	AmountTotal        int64
	AmountUsedBefore   int64
	AmountUsedAfter    int64
}

// ExpireDueSubscriptions marks expired subscriptions and handles group downgrade.
func ExpireDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("status = ? AND end_time > 0 AND end_time <= ?", "active", now).
		Order("end_time asc, id asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	expiredCount := 0
	userIds := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if sub.UserId > 0 {
			userIds[sub.UserId] = struct{}{}
		}
	}
	for userId := range userIds {
		cacheGroup := ""
		err := DB.Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?", userId, "active", now).
				Updates(map[string]interface{}{
					"status":     "expired",
					"updated_at": common.GetTimestamp(),
				})
			if res.Error != nil {
				return res.Error
			}
			expiredCount += int(res.RowsAffected)

			// Select the most recent expired upgrade and restore its previous group
			// unless an active subscription still grants the current group.
			var lastExpired UserSubscription
			expiredQuery := tx.Where("user_id = ? AND status = ? AND upgrade_group <> ''",
				userId, "expired").
				Order("end_time desc, id desc").
				Limit(1).
				Find(&lastExpired)
			if expiredQuery.Error != nil || expiredQuery.RowsAffected == 0 {
				return nil
			}
			upgradeGroup := strings.TrimSpace(lastExpired.UpgradeGroup)
			prevGroup := strings.TrimSpace(lastExpired.PrevUserGroup)
			if upgradeGroup == "" || prevGroup == "" {
				return nil
			}
			currentGroup, err := getUserGroupByIdTx(tx, userId)
			if err != nil {
				return err
			}
			if currentGroup != upgradeGroup || currentGroup == prevGroup {
				return nil
			}
			// Keep the current group only when another active subscription grants
			// that exact group. Any other active upgrade must not retain a higher
			// group from the subscription that just expired.
			activeQuery := tx.Where("user_id = ? AND status = ? AND end_time > ? AND upgrade_group <> ''",
				userId, "active", now).
				Where("TRIM(upgrade_group) = ?", currentGroup).
				Limit(1).
				Find(&UserSubscription{})
			if activeQuery.Error != nil {
				return activeQuery.Error
			}
			if activeQuery.RowsAffected > 0 {
				return nil
			}
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", prevGroup).Error; err != nil {
				return err
			}
			cacheGroup = prevGroup
			return nil
		})
		if err != nil {
			return expiredCount, err
		}
		if cacheGroup != "" {
			invalidateSubscriptionUserCache(userId, "subscription expiration")
		}
	}
	return expiredCount, nil
}

// SubscriptionPreConsumeRecord stores idempotent pre-consume operations per request.
type SubscriptionPreConsumeRecord struct {
	Id                        int    `json:"id"`
	RequestId                 string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId                    int    `json:"user_id" gorm:"index"`
	TokenId                   int    `json:"token_id" gorm:"index"`
	UserSubscriptionId        int    `json:"user_subscription_id" gorm:"index"`
	SubscriptionLastResetTime int64  `json:"subscription_last_reset_time" gorm:"bigint;not null;default:0"`
	ModelName                 string `json:"model_name" gorm:"type:varchar(191)"`
	QuotaType                 int    `json:"quota_type"`
	PreConsumed               int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	Status                    string `json:"status" gorm:"type:varchar(32);index"` // consumed/refunded
	CreatedAt                 int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt                 int64  `json:"updated_at" gorm:"bigint;index"`
}

func (r *SubscriptionPreConsumeRecord) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *SubscriptionPreConsumeRecord) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func maybeResetUserSubscriptionWithPlanTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	if sub.NextResetTime > 0 && sub.NextResetTime > now {
		return nil
	}
	if NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetNever {
		return nil
	}
	baseUnix := sub.LastResetTime
	if baseUnix <= 0 {
		baseUnix = sub.StartTime
	}
	base := time.Unix(baseUnix, 0)
	next := calcNextResetTime(base, plan, sub.EndTime)
	advanced := false
	for next > 0 && next <= now {
		advanced = true
		base = time.Unix(next, 0)
		next = calcNextResetTime(base, plan, sub.EndTime)
	}
	if !advanced {
		if sub.NextResetTime == 0 && next > 0 {
			sub.NextResetTime = next
			sub.LastResetTime = base.Unix()
			return tx.Save(sub).Error
		}
		return nil
	}
	sub.AmountUsed = 0
	sub.LastResetTime = base.Unix()
	sub.NextResetTime = next
	return tx.Save(sub).Error
}

// PreConsumeUserSubscription pre-consumes from any active subscription total quota.
func PreConsumeUserSubscription(requestId string, userId int, modelName string, quotaType int, amount int64) (*SubscriptionPreConsumeResult, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	now := GetDBTimestamp()

	var returnValue SubscriptionPreConsumeResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := claimBillingPreConsumeSourceTx(tx, BillingPreConsumeSelection{
			RequestID: requestId, Source: BillingSettlementSourceSubscription,
			UserID: userId, ModelName: modelName,
			RequestedQuota: amount, EffectiveQuota: amount,
		}); err != nil {
			return err
		}
		var err error
		returnValue, _, err = preConsumeUserSubscriptionTx(tx, requestId, userId, 0, modelName, quotaType, now, amount)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &returnValue, nil
}

// PreConsumeTokenAndUserSubscription atomically reserves subscription and
// token quota. Existing request IDs are returned without applying the token
// mutation again.
func PreConsumeTokenAndUserSubscription(requestId string, userId int, tokenId int, tokenKey string, modelName string, quotaType int, amount int64) (*SubscriptionPreConsumeResult, error) {
	if tokenId <= 0 {
		return nil, errors.New("invalid tokenId")
	}
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	now := GetDBTimestamp()
	var returnValue SubscriptionPreConsumeResult
	var cacheTask CacheInvalidationTask
	resolvedTokenKey := tokenKey
	alreadyConsumed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := claimBillingPreConsumeSourceTx(tx, BillingPreConsumeSelection{
			RequestID: requestId, Source: BillingSettlementSourceSubscription,
			UserID: userId, TokenID: tokenId, ModelName: modelName,
			RequestedQuota: amount, EffectiveQuota: amount,
		}); err != nil {
			return err
		}
		var err error
		returnValue, alreadyConsumed, err = preConsumeUserSubscriptionTx(tx, requestId, userId, tokenId, modelName, quotaType, now, amount)
		if err != nil || alreadyConsumed {
			if alreadyConsumed {
				var token Token
				if tokenErr := tx.Unscoped().Select("key").First(&token, tokenId).Error; tokenErr == nil {
					resolvedTokenKey = token.Key
				}
			}
			return err
		}
		result := tx.Model(&Token{}).
			Where("id = ? AND user_id = ? AND (unlimited_quota = ? OR remain_quota >= ?)", tokenId, userId, true, amount).
			Updates(map[string]interface{}{
				"remain_quota":  gorm.Expr("remain_quota - ?", amount),
				"used_quota":    gorm.Expr("used_quota + ?", amount),
				"accessed_time": common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: id=%d, need=%d", ErrTokenQuotaInsufficient, tokenId, amount)
		}
		var token Token
		if err := tx.First(&token, tokenId).Error; err != nil {
			return err
		}
		resolvedTokenKey = token.Key
		cacheTask, err = stageTokenCacheInvalidationTx(tx, resolvedTokenKey, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	if alreadyConsumed {
		invalidateTokenQuotaCache(resolvedTokenKey)
	} else {
		dispatchStagedCacheInvalidation(cacheTask)
	}
	return &returnValue, nil
}

func preConsumeUserSubscriptionTx(tx *gorm.DB, requestId string, userId int, tokenId int, modelName string, quotaType int, now int64, amount int64) (SubscriptionPreConsumeResult, bool, error) {
	returnValue := SubscriptionPreConsumeResult{}
	var existing SubscriptionPreConsumeRecord
	query := tx.Where("request_id = ?", requestId).Limit(1).Find(&existing)
	if query.Error != nil {
		return returnValue, false, query.Error
	}
	if query.RowsAffected > 0 {
		if err := validateSubscriptionPreConsumeReplay(existing, userId, tokenId, modelName, quotaType, amount); err != nil {
			return returnValue, true, err
		}
		if existing.Status == "refunded" {
			return returnValue, true, errors.New("subscription pre-consume already refunded")
		}
		var sub UserSubscription
		if err := tx.Where("id = ?", existing.UserSubscriptionId).First(&sub).Error; err != nil {
			return returnValue, true, err
		}
		if sub.UserId != userId {
			return returnValue, true, errors.New("subscription pre-consume user identity mismatch")
		}
		if err := validateSubscriptionPreConsumePeriod(existing, sub); err != nil {
			return returnValue, true, err
		}
		returnValue.UserSubscriptionId = sub.Id
		returnValue.PreConsumed = existing.PreConsumed
		returnValue.AmountTotal = sub.AmountTotal
		returnValue.AmountUsedBefore = sub.AmountUsed
		returnValue.AmountUsedAfter = sub.AmountUsed
		return returnValue, true, nil
	}

	var subs []UserSubscription
	if err := withRowLock(tx).
		Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Order("end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return returnValue, false, err
	}
	if len(subs) == 0 {
		return returnValue, false, errors.New("no active subscription")
	}
	for _, candidate := range subs {
		sub := candidate
		plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId)
		if err != nil {
			return returnValue, false, err
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, &sub, plan, now); err != nil {
			return returnValue, false, err
		}
		usedBefore := sub.AmountUsed
		if sub.AmountTotal > 0 && sub.AmountTotal-usedBefore < amount {
			continue
		}
		record := &SubscriptionPreConsumeRecord{
			RequestId:                 requestId,
			UserId:                    userId,
			TokenId:                   tokenId,
			UserSubscriptionId:        sub.Id,
			SubscriptionLastResetTime: sub.LastResetTime,
			ModelName:                 modelName,
			QuotaType:                 quotaType,
			PreConsumed:               amount,
			Status:                    "consumed",
		}
		if err := tx.Create(record).Error; err != nil {
			var duplicate SubscriptionPreConsumeRecord
			if duplicateErr := tx.Where("request_id = ?", requestId).First(&duplicate).Error; duplicateErr == nil {
				if replayErr := validateSubscriptionPreConsumeReplay(duplicate, userId, tokenId, modelName, quotaType, amount); replayErr != nil {
					return returnValue, true, replayErr
				}
				if duplicate.Status == "refunded" {
					return returnValue, true, errors.New("subscription pre-consume already refunded")
				}
				var duplicateSub UserSubscription
				if err := tx.Where("id = ?", duplicate.UserSubscriptionId).First(&duplicateSub).Error; err != nil {
					return returnValue, true, err
				}
				if duplicateSub.UserId != userId {
					return returnValue, true, errors.New("subscription pre-consume user identity mismatch")
				}
				if err := validateSubscriptionPreConsumePeriod(duplicate, duplicateSub); err != nil {
					return returnValue, true, err
				}
				returnValue.UserSubscriptionId = duplicateSub.Id
				returnValue.PreConsumed = duplicate.PreConsumed
				returnValue.AmountTotal = duplicateSub.AmountTotal
				returnValue.AmountUsedBefore = duplicateSub.AmountUsed
				returnValue.AmountUsedAfter = duplicateSub.AmountUsed
				return returnValue, true, nil
			}
			return returnValue, false, err
		}
		updateQuery := tx.Model(&UserSubscription{}).
			Where("id = ? AND status = ? AND end_time > ?", sub.Id, "active", now).
			Where("amount_total <= 0 OR amount_used + ? <= amount_total", amount)
		result := updateQuery.Updates(map[string]interface{}{
			"amount_used": gorm.Expr("amount_used + ?", amount),
			"updated_at":  common.GetTimestamp(),
		})
		if result.Error != nil {
			return returnValue, false, result.Error
		}
		if result.RowsAffected != 1 {
			if err := tx.Delete(record).Error; err != nil {
				return returnValue, false, err
			}
			continue
		}
		var updatedSub UserSubscription
		if err := tx.Where("id = ?", sub.Id).First(&updatedSub).Error; err != nil {
			return returnValue, false, err
		}
		returnValue.UserSubscriptionId = sub.Id
		returnValue.PreConsumed = amount
		returnValue.AmountTotal = updatedSub.AmountTotal
		returnValue.AmountUsedBefore = usedBefore
		returnValue.AmountUsedAfter = updatedSub.AmountUsed
		return returnValue, false, nil
	}
	return returnValue, false, fmt.Errorf("%w: need=%d", ErrSubscriptionQuotaInsufficient, amount)
}

func validateSubscriptionPreConsumeReplay(existing SubscriptionPreConsumeRecord, userId int, tokenId int, modelName string, quotaType int, amount int64) error {
	if existing.UserId != userId ||
		existing.TokenId != tokenId ||
		existing.ModelName != modelName ||
		existing.QuotaType != quotaType ||
		existing.PreConsumed != amount {
		return fmt.Errorf("subscription pre-consume request reused with different identity or parameters: request=%s", existing.RequestId)
	}
	return nil
}

func validateSubscriptionPreConsumePeriod(record SubscriptionPreConsumeRecord, sub UserSubscription) error {
	if record.SubscriptionLastResetTime != sub.LastResetTime {
		return fmt.Errorf("%w: request=%s", ErrSubscriptionSettlementPeriodChanged, record.RequestId)
	}
	return nil
}

// RefundSubscriptionPreConsume is idempotent and refunds pre-consumed subscription quota by requestId.
func RefundSubscriptionPreConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := withRowLock(tx).
			Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		var sub UserSubscription
		if err := withRowLock(tx).Where("id = ?", record.UserSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		if err := validateSubscriptionPreConsumePeriod(record, sub); err != nil {
			return err
		}
		result := tx.Model(&SubscriptionPreConsumeRecord{}).
			Where("id = ? AND status <> ?", record.Id, "refunded").
			Updates(map[string]interface{}{
				"status":     "refunded",
				"updated_at": common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if record.PreConsumed <= 0 {
			return nil
		}
		applied, err := postConsumeUserSubscriptionDeltaTx(tx, record.UserSubscriptionId, -record.PreConsumed)
		if err != nil {
			return err
		}
		if applied != -record.PreConsumed {
			return fmt.Errorf("subscription refund was clamped: requested=%d applied=%d", -record.PreConsumed, applied)
		}
		return nil
	})
}

// ResetDueSubscriptions resets subscriptions whose next_reset_time has passed.
func ResetDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("next_reset_time > 0 AND next_reset_time <= ? AND status = ?", now, "active").
		Order("next_reset_time asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, sub := range subs {
		subCopy := sub
		plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
		if err != nil || plan == nil {
			continue
		}
		err = DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscription
			if err := withRowLock(tx).
				Where("id = ? AND next_reset_time > 0 AND next_reset_time <= ?", subCopy.Id, now).
				First(&locked).Error; err != nil {
				return nil
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &locked, plan, now); err != nil {
				return err
			}
			resetCount++
			return nil
		})
		if err != nil {
			return resetCount, err
		}
	}
	return resetCount, nil
}

// CleanupSubscriptionPreConsumeRecords removes only old replay tombstones whose
// linked billing settlements have reached terminal applied/manual states.
// olderThanSeconds is retained for the scheduled cleanup API; non-positive
// values disable deletion, and pending settlement rows are always preserved.
func CleanupSubscriptionPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		return 0, nil
	}
	cutoff := common.GetTimestamp() - olderThanSeconds
	if cutoff <= 0 {
		return 0, nil
	}
	terminalStatuses := []string{BillingSettlementStatusApplied, BillingSettlementStatusManual}
	nonterminalSettlements := DB.Model(&BillingSettlement{}).
		Select("1").
		Where("billing_settlements.subscription_pre_consume_request_id = subscription_pre_consume_records.request_id").
		Where("billing_settlements.status NOT IN ?", terminalStatuses)
	terminalSettlements := DB.Model(&BillingSettlement{}).
		Select("1").
		Where("billing_settlements.subscription_pre_consume_request_id = subscription_pre_consume_records.request_id").
		Where("billing_settlements.status IN ?", terminalStatuses)
	result := DB.
		Where("created_at < ?", cutoff).
		Where("EXISTS (?)", terminalSettlements).
		Where("NOT EXISTS (?)", nonterminalSettlements).
		Delete(&SubscriptionPreConsumeRecord{})
	return result.RowsAffected, result.Error
}

type SubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}

func GetSubscriptionPlanInfoByUserSubscriptionId(userSubscriptionId int) (*SubscriptionPlanInfo, error) {
	if userSubscriptionId <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}
	cacheKey := fmt.Sprintf("sub:%d", userSubscriptionId)
	if cached, found, err := getSubscriptionPlanInfoCache().Get(cacheKey); err == nil && found {
		return &cached, nil
	}
	var sub UserSubscription
	if err := DB.Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
		return nil, err
	}
	plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
	if err != nil {
		return nil, err
	}
	info := &SubscriptionPlanInfo{
		PlanId:    sub.PlanId,
		PlanTitle: plan.Title,
	}
	_ = getSubscriptionPlanInfoCache().SetWithTTL(cacheKey, *info, subscriptionPlanInfoCacheTTL())
	return info, nil
}

// Update subscription used amount by delta (positive consume more, negative refund).
// It returns the actual applied delta because negative deltas are floored at zero.
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) (int64, error) {
	if userSubscriptionId <= 0 {
		return 0, errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return 0, nil
	}
	var applied int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		applied, err = postConsumeUserSubscriptionDeltaTx(tx, userSubscriptionId, delta)
		return err
	})
	return applied, err
}

func postConsumeUserSubscriptionDeltaTx(tx *gorm.DB, userSubscriptionId int, delta int64) (int64, error) {
	if tx == nil {
		return 0, errors.New("invalid transaction")
	}
	var sub UserSubscription
	if err := withRowLock(tx).
		Where("id = ?", userSubscriptionId).
		First(&sub).Error; err != nil {
		return 0, err
	}
	if delta > 0 {
		newUsed := sub.AmountUsed + delta
		if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
			return 0, fmt.Errorf("%w: used=%d total=%d", ErrSubscriptionQuotaInsufficient, newUsed, sub.AmountTotal)
		}
		updateQuery := tx.Model(&UserSubscription{}).Where("id = ?", userSubscriptionId)
		if sub.AmountTotal > 0 {
			updateQuery = updateQuery.Where("amount_used + ? <= amount_total", delta)
		}
		result := updateQuery.Updates(map[string]interface{}{
			"amount_used": gorm.Expr("amount_used + ?", delta),
			"updated_at":  common.GetTimestamp(),
		})
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected != 1 {
			return 0, fmt.Errorf("%w: used=%d total=%d", ErrSubscriptionQuotaInsufficient, newUsed, sub.AmountTotal)
		}
		return delta, nil
	}

	if sub.AmountUsed <= 0 {
		return 0, nil
	}
	applied := delta
	if -delta > sub.AmountUsed {
		applied = -sub.AmountUsed
	}
	result := tx.Model(&UserSubscription{}).
		Where("id = ?", userSubscriptionId).
		Updates(map[string]interface{}{
			"amount_used": gorm.Expr("CASE WHEN amount_used + ? < 0 THEN 0 ELSE amount_used + ? END", delta, delta),
			"updated_at":  common.GetTimestamp(),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, errors.New("subscription update conflict")
	}
	return applied, nil
}
