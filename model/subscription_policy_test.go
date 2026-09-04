package model

import (
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionPlanNormalizeDefaultsAllowsWalletOverflowByDefault(t *testing.T) {
	var plan SubscriptionPlan
	plan.NormalizeDefaults()
	require.NotNil(t, plan.AllowWalletOverflow)
	assert.True(t, *plan.AllowWalletOverflow)

	allow := false
	plan.AllowWalletOverflow = &allow
	plan.NormalizeDefaults()
	require.NotNil(t, plan.AllowWalletOverflow)
	assert.False(t, *plan.AllowWalletOverflow)
}

func TestCreateUserSubscriptionSnapshotsWalletOverflowAndDowngradePolicy(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9801, Username: "subscription-policy-snapshot", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	allow := false
	plan := &SubscriptionPlan{
		Id: 9802, Title: "Strict", Enabled: true, TotalAmount: 100,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		AllowWalletOverflow: &allow, UpgradeGroup: "svip", DowngradeGroup: "vip",
	}
	require.NoError(t, DB.Create(plan).Error)

	var created *UserSubscription
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		created, err = CreateUserSubscriptionFromPlanTx(tx, user.Id, plan, "test")
		return err
	}))
	require.NotNil(t, created)
	assert.False(t, created.AllowWalletOverflow)
	assert.Equal(t, "vip", created.DowngradeGroup)

	// Later plan edits must not alter the already purchased snapshot.
	allowLater := true
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"allow_wallet_overflow": allowLater,
		"downgrade_group":       "default",
	}).Error)
	var stored UserSubscription
	require.NoError(t, DB.First(&stored, created.Id).Error)
	assert.False(t, stored.AllowWalletOverflow)
	assert.Equal(t, "vip", stored.DowngradeGroup)
	assert.Greater(t, stored.StartTime, int64(0))
	assert.Greater(t, stored.EndTime, now)
}

func TestCreateUserSubscriptionDefaultsWalletOverflowWhenPlanUnset(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 9811, Username: "subscription-policy-default", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Id: 9812, Title: "Legacy", Enabled: true, TotalAmount: 100,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		AllowWalletOverflow: nil,
	}
	require.NoError(t, DB.Create(plan).Error)

	var sub *UserSubscription
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sub, err = CreateUserSubscriptionFromPlanTx(tx, user.Id, plan, "test")
		return err
	}))
	require.NotNil(t, sub)
	assert.True(t, sub.AllowWalletOverflow)
}

func TestUserActiveSubscriptionsAllowWalletOverflowBlocksOnAnyStrictSubscription(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9821, Username: "subscription-policy-conflict", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	truePlan := &SubscriptionPlan{Id: 9822, Title: "Allowed", Enabled: true, DurationUnit: SubscriptionDurationMonth, DurationValue: 1}
	falsePlan := &SubscriptionPlan{Id: 9823, Title: "Strict", Enabled: true, DurationUnit: SubscriptionDurationMonth, DurationValue: 1}
	allow := true
	deny := false
	truePlan.AllowWalletOverflow = &allow
	falsePlan.AllowWalletOverflow = &deny
	require.NoError(t, DB.Create(truePlan).Error)
	require.NoError(t, DB.Create(falsePlan).Error)
	require.NoError(t, DB.Create(&UserSubscription{Id: 9824, UserId: user.Id, PlanId: truePlan.Id, Status: "active", StartTime: now - 10, EndTime: now + 3600, AllowWalletOverflow: true}).Error)
	require.NoError(t, DB.Create(&UserSubscription{Id: 9825, UserId: user.Id, PlanId: falsePlan.Id, Status: "active", StartTime: now - 10, EndTime: now + 3600, AllowWalletOverflow: false}).Error)

	allowed, err := UserActiveSubscriptionsAllowWalletOverflow(user.Id)
	require.NoError(t, err)
	assert.False(t, allowed)

	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 9825).Update("allow_wallet_overflow", true).Error)
	allowed, err = UserActiveSubscriptionsAllowWalletOverflow(user.Id)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestDowngradeUserGroupUsesExplicitSnapshotBeforePreviousGroup(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9831, Username: "subscription-explicit-downgrade", Group: "svip", Status: common.UserStatusEnabled}
	sub := &UserSubscription{Id: 9832, UserId: user.Id, Status: "cancelled", EndTime: now + time.Hour.Milliseconds()/1000, UpgradeGroup: "svip", PrevUserGroup: "vip", DowngradeGroup: "default"}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(sub).Error)

	var target string
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		target, err = downgradeUserGroupForSubscriptionTx(tx, sub, now)
		return err
	}))
	assert.Equal(t, "default", target)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "default", stored.Group)
}

func TestAdminInvalidateUserSubscriptionUsesExplicitDowngradeWithoutUpgrade(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9833, Username: "subscription-cancel-explicit-downgrade", Group: "svip", Status: common.UserStatusEnabled}
	sub := &UserSubscription{Id: 9834, UserId: user.Id, Status: "active", EndTime: now + 3600, DowngradeGroup: "default"}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(sub).Error)

	message, err := AdminInvalidateUserSubscription(sub.Id)
	require.NoError(t, err)
	assert.Contains(t, message, "default")
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "default", stored.Group)
}

func TestAdminDeleteUserSubscriptionUsesExplicitDowngradeWithoutUpgrade(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9835, Username: "subscription-delete-explicit-downgrade", Group: "svip", Status: common.UserStatusEnabled}
	sub := &UserSubscription{Id: 9836, UserId: user.Id, Status: "active", EndTime: now + 3600, DowngradeGroup: "default"}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(sub).Error)

	message, err := AdminDeleteUserSubscription(sub.Id)
	require.NoError(t, err)
	assert.Contains(t, message, "default")
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "default", stored.Group)
	assert.ErrorIs(t, DB.First(&UserSubscription{}, sub.Id).Error, gorm.ErrRecordNotFound)
}

func TestExpireDueSubscriptionsUsesExplicitDowngradeWithoutUpgrade(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9837, Username: "subscription-expiry-explicit-downgrade", Group: "svip", Status: common.UserStatusEnabled}
	sub := &UserSubscription{Id: 9838, UserId: user.Id, Status: "active", EndTime: now - 1, DowngradeGroup: "default"}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(sub).Error)

	count, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "default", stored.Group)
	assert.Equal(t, "expired", getSubscriptionResetSub(t, sub.Id).Status)
}

func TestExpireDueSubscriptionsUsesExplicitDowngradeSnapshot(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9841, Username: "subscription-explicit-expiry", Group: "svip", Status: common.UserStatusEnabled}
	expired := &UserSubscription{
		Id: 9842, UserId: user.Id, Status: "active", EndTime: now - 1,
		UpgradeGroup: "svip", PrevUserGroup: "vip", DowngradeGroup: "default",
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(expired).Error)

	count, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "default", stored.Group)
	assert.Equal(t, "expired", getSubscriptionResetSub(t, expired.Id).Status)
}

func TestExpireDueSubscriptionsDoesNotReplayHistoricalDowngradePolicy(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	user := &User{Id: 9851, Username: "subscription-no-policy-expiry", Group: "svip", Status: common.UserStatusEnabled}
	historical := &UserSubscription{
		Id: 9852, UserId: user.Id, Status: "expired", EndTime: now - 3600,
		DowngradeGroup: "default",
	}
	dueWithoutPolicy := &UserSubscription{
		Id: 9853, UserId: user.Id, Status: "active", EndTime: now - 1,
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(historical).Error)
	require.NoError(t, DB.Create(dueWithoutPolicy).Error)

	count, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "svip", stored.Group)
	assert.Equal(t, "expired", getSubscriptionResetSub(t, dueWithoutPolicy.Id).Status)
}
