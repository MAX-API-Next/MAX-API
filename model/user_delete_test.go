package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserDeleteRejectsRootUserInModelLayer(t *testing.T) {
	truncateTables(t)
	root := User{Id: 9101, Username: "root-delete-guard", Password: "password123", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&root).Error)

	err := (&User{Id: root.Id}).Delete()
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", root.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestUserHardDeleteRejectsRootUserInModelLayer(t *testing.T) {
	truncateTables(t)
	root := User{Id: 9102, Username: "root-hard-delete-guard", Password: "password123", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&root).Error)

	err := HardDeleteUserById(root.Id)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", root.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestUserSoftDeleteInvalidatesCredentialsAndCancelsActiveSubscriptions(t *testing.T) {
	truncateTables(t)
	accessToken := "soft-delete-access-token"
	now := common.GetTimestamp()
	user := User{
		Id:          9103,
		Username:    "soft-delete-cleanup",
		Password:    "password123",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AccessToken: &accessToken,
		AffCode:     "soft-delete-cleanup",
	}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{Id: 19103, UserId: user.Id, Key: "soft-delete-token", Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	binding := UserOAuthBinding{UserId: user.Id, ProviderId: 9103, ProviderUserId: "soft-delete-provider-user"}
	require.NoError(t, DB.Create(&binding).Error)
	override := AuthzUserOverride{UserID: user.Id, Resource: "channel", Action: "sensitive_write", Allowed: true}
	require.NoError(t, DB.Create(&override).Error)
	subscription := UserSubscription{
		Id:          29103,
		UserId:      user.Id,
		PlanId:      39103,
		AmountTotal: 100,
		StartTime:   now - 60,
		EndTime:     now + 3600,
		Status:      "active",
	}
	require.NoError(t, DB.Create(&subscription).Error)

	deleteStartedAt := common.GetTimestamp()
	require.NoError(t, (&User{Id: user.Id}).Delete())
	deleteFinishedAt := common.GetTimestamp()

	var storedUser User
	require.NoError(t, DB.Unscoped().First(&storedUser, user.Id).Error)
	assert.True(t, storedUser.DeletedAt.Valid)
	assert.Nil(t, storedUser.AccessToken)

	var storedToken Token
	require.NoError(t, DB.Unscoped().First(&storedToken, token.Id).Error)
	assert.False(t, storedToken.DeletedAt.Valid)
	assert.Equal(t, common.TokenStatusDisabled, storedToken.Status)

	var bindingCount int64
	require.NoError(t, DB.Model(&UserOAuthBinding{}).Where("user_id = ?", user.Id).Count(&bindingCount).Error)
	assert.Zero(t, bindingCount)
	var overrideCount int64
	require.NoError(t, DB.Model(&AuthzUserOverride{}).Where("user_id = ?", user.Id).Count(&overrideCount).Error)
	assert.Zero(t, overrideCount)

	var storedSubscription UserSubscription
	require.NoError(t, DB.First(&storedSubscription, subscription.Id).Error)
	assert.Equal(t, "cancelled", storedSubscription.Status)
	assert.GreaterOrEqual(t, storedSubscription.EndTime, deleteStartedAt)
	assert.LessOrEqual(t, storedSubscription.EndTime, deleteFinishedAt)
}

func TestUserHardDeleteRemovesCredentialsAndCancelsActiveSubscriptions(t *testing.T) {
	truncateTables(t)
	accessToken := "hard-delete-access-token"
	now := common.GetTimestamp()
	user := User{
		Id:          9104,
		Username:    "hard-delete-cleanup",
		Password:    "password123",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AccessToken: &accessToken,
		AffCode:     "hard-delete-cleanup",
	}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{Id: 19104, UserId: user.Id, Key: "hard-delete-token", Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	binding := UserOAuthBinding{UserId: user.Id, ProviderId: 9104, ProviderUserId: "hard-delete-provider-user"}
	require.NoError(t, DB.Create(&binding).Error)
	override := AuthzUserOverride{UserID: user.Id, Resource: "channel", Action: "sensitive_write", Allowed: true}
	require.NoError(t, DB.Create(&override).Error)
	subscription := UserSubscription{
		Id:          29104,
		UserId:      user.Id,
		PlanId:      39104,
		AmountTotal: 100,
		StartTime:   now - 60,
		EndTime:     now + 3600,
		Status:      "active",
	}
	require.NoError(t, DB.Create(&subscription).Error)

	deleteStartedAt := common.GetTimestamp()
	require.NoError(t, HardDeleteUserById(user.Id))
	deleteFinishedAt := common.GetTimestamp()

	var userCount int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Count(&userCount).Error)
	assert.Zero(t, userCount)

	var tokenCount int64
	require.NoError(t, DB.Unscoped().Model(&Token{}).Where("id = ?", token.Id).Count(&tokenCount).Error)
	assert.Zero(t, tokenCount)

	var bindingCount int64
	require.NoError(t, DB.Model(&UserOAuthBinding{}).Where("user_id = ?", user.Id).Count(&bindingCount).Error)
	assert.Zero(t, bindingCount)
	var overrideCount int64
	require.NoError(t, DB.Unscoped().Model(&AuthzUserOverride{}).Where("user_id = ?", user.Id).Count(&overrideCount).Error)
	assert.Zero(t, overrideCount)

	var storedSubscription UserSubscription
	require.NoError(t, DB.First(&storedSubscription, subscription.Id).Error)
	assert.Equal(t, "cancelled", storedSubscription.Status)
	assert.GreaterOrEqual(t, storedSubscription.EndTime, deleteStartedAt)
	assert.LessOrEqual(t, storedSubscription.EndTime, deleteFinishedAt)
}
