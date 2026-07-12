package controller

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserSettingControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldLOGDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL

	common.RedisEnabled = false
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = oldDB
		model.LOG_DB = oldLOGDB
		common.RedisEnabled = oldRedisEnabled
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
	})

	return db
}

func TestUpdateUserSettingPreservesUnrelatedSettings(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)

	initialSettings := dto.UserSetting{
		NotifyType:                       dto.NotifyTypeWebhook,
		QuotaWarningThreshold:            0.5,
		WebhookUrl:                       "https://example.com/old-webhook",
		WebhookSecret:                    "old-secret",
		BarkUrl:                          "https://example.com/old-bark",
		GotifyUrl:                        "https://example.com/old-gotify",
		GotifyToken:                      "old-token",
		GotifyPriority:                   7,
		UpstreamModelUpdateNotifyEnabled: true,
		SidebarModules:                   `{"chat":{"enabled":true}}`,
		BillingPreference:                "subscription",
		Language:                         "zh",
	}
	initialSettingBytes, err := common.Marshal(initialSettings)
	require.NoError(t, err)

	user := model.User{
		Id:       1001,
		Username: "notification-setting-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Setting:  string(initialSettingBytes),
	}
	require.NoError(t, db.Create(&user).Error)

	payload := map[string]any{
		"notify_type":                    dto.NotifyTypeEmail,
		"quota_warning_threshold":        0.75,
		"notification_email":             "new@example.com",
		"accept_unset_model_ratio_model": true,
		"record_ip_log":                  true,
	}
	payloadBytes, err := common.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", bytes.NewReader(payloadBytes))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", user.Id)

	UpdateUserSetting(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var got model.User
	require.NoError(t, db.First(&got, user.Id).Error)
	setting := got.GetSetting()
	assert.Equal(t, "zh", setting.Language)
	assert.Equal(t, `{"chat":{"enabled":true}}`, setting.SidebarModules)
	assert.Equal(t, "subscription", setting.BillingPreference)
	assert.True(t, setting.UpstreamModelUpdateNotifyEnabled)
	assert.Equal(t, dto.NotifyTypeEmail, setting.NotifyType)
	assert.Equal(t, 0.75, setting.QuotaWarningThreshold)
	assert.Equal(t, "new@example.com", setting.NotificationEmail)
	assert.True(t, setting.AcceptUnsetRatioModel)
	assert.True(t, setting.RecordIpLog)
	assert.Empty(t, setting.WebhookUrl)
	assert.Empty(t, setting.WebhookSecret)
	assert.Empty(t, setting.BarkUrl)
	assert.Empty(t, setting.GotifyUrl)
	assert.Empty(t, setting.GotifyToken)
	assert.Zero(t, setting.GotifyPriority)
}

func TestNormalizeNotificationEmailRejectsMalformedAddress(t *testing.T) {
	validEmail, ok := normalizeNotificationEmail("new@example.com")
	require.True(t, ok)
	require.Equal(t, "new@example.com", validEmail)

	for _, email := range []string{"a@", "@b", "@.", "a@b", "Display <new@example.com>"} {
		_, ok := normalizeNotificationEmail(email)
		require.False(t, ok, "email should be rejected: %s", email)
	}
}

func TestTopUpLockReleaseDeletesIdleEntry(t *testing.T) {
	topUpLocks.Range(func(key, value any) bool {
		topUpLocks.Delete(key)
		return true
	})

	entry := acquireTopUpLock(1001)
	require.True(t, entry.lock.TryLock())
	entry.lock.Unlock()
	releaseTopUpLock(1001, entry)

	_, ok := topUpLocks.Load(1001)
	require.False(t, ok)
}

func TestTopUpLockReleaseKeepsReferencedEntry(t *testing.T) {
	topUpLocks.Range(func(key, value any) bool {
		topUpLocks.Delete(key)
		return true
	})

	first := acquireTopUpLock(1002)
	second := acquireTopUpLock(1002)
	require.Same(t, first, second)

	releaseTopUpLock(1002, first)
	_, ok := topUpLocks.Load(1002)
	require.True(t, ok)

	releaseTopUpLock(1002, second)
	_, ok = topUpLocks.Load(1002)
	require.False(t, ok)
}

func TestRegisterConsumesEmailVerificationCode(t *testing.T) {
	setupUserSettingControllerTestDB(t)

	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalQuotaForNewUser := common.QuotaForNewUser
	originalGenerateDefaultToken := constant.GenerateDefaultToken
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = true
	common.QuotaForNewUser = 0
	constant.GenerateDefaultToken = false
	t.Cleanup(func() {
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
		common.QuotaForNewUser = originalQuotaForNewUser
		constant.GenerateDefaultToken = originalGenerateDefaultToken
	})

	email := "register-code@example.com"
	code := "123456"
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	payloadBytes, err := common.Marshal(model.User{
		Username:         "registercode",
		Password:         "password123",
		Email:            email,
		VerificationCode: code,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(payloadBytes))
	ctx.Request.Header.Set("Content-Type", "application/json")

	Register(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose))
}

func TestQuotaBoundsValidation(t *testing.T) {
	legacyInt32Max := int64(1<<31 - 1)
	aboveLegacyInt32Max := legacyInt32Max + 1
	javascriptSafeIntegerMax := int64(1<<53 - 1)
	maxQuota := maxUserQuotaValue
	overflowingHalfMax := maxQuota/2 + 1

	require.Equal(t, javascriptSafeIntegerMax, maxQuota)
	require.True(t, isValidQuotaOverride(0))
	require.True(t, isValidQuotaOverride(aboveLegacyInt32Max))
	require.True(t, isValidQuotaOverride(maxQuota))
	require.False(t, isValidQuotaOverride(maxQuota+1))
	require.False(t, isValidQuotaOverride(-1))

	require.True(t, isValidQuotaAddition(aboveLegacyInt32Max, 1))
	require.True(t, isValidQuotaAddition(0, aboveLegacyInt32Max))
	require.True(t, isValidQuotaAddition(maxQuota-1, 1))
	require.True(t, isValidQuotaAddition(0, maxQuota))
	require.False(t, isValidQuotaAddition(maxQuota, 1))
	require.False(t, isValidQuotaAddition(1, maxQuota))
	require.False(t, isValidQuotaAddition(overflowingHalfMax, overflowingHalfMax))
	require.False(t, isValidQuotaAddition(maxQuota, maxQuota))
	require.False(t, isValidQuotaAddition(0, 0))

	require.True(t, isValidQuotaSubtraction(-maxQuota+1, 1))
	require.True(t, isValidQuotaSubtraction(0, maxQuota))
	require.False(t, isValidQuotaSubtraction(-maxQuota, 1))
	require.False(t, isValidQuotaSubtraction(0, maxQuota+1))
	require.False(t, isValidQuotaSubtraction(0, 0))
}

func TestResetPasswordKeepsVerificationCodeWhenPasswordUpdateFails(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	user := model.User{
		Username: "reset-failure-user",
		Password: "ExistingPassword123",
		Email:    "reset-failure@example.com",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&user).Error)

	wantErr := errors.New("forced password update failure")
	const callbackName = "test:force_password_reset_failure"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		tx.AddError(wantErr)
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Update().Remove(callbackName))
	})

	code := "reset-code"
	common.RegisterVerificationCodeWithKey(user.Email, code, common.PasswordResetPurpose)
	t.Cleanup(func() { common.DeleteKey(user.Email, common.PasswordResetPurpose) })
	payload, err := common.Marshal(PasswordResetRequest{Email: user.Email, Token: code})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ResetPassword(ctx)

	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.True(t, common.VerifyCodeWithKey(user.Email, code, common.PasswordResetPurpose))
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, user.Password, stored.Password)
}

func TestManageUserRejectsQuotaValueAboveInt64Max(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(
		`{"id":1,"action":"add_quota","mode":"override","value":9223372036854775808}`,
	))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ManageUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestManageUserRejectsQuotaValueAboveJavaScriptSafeInteger(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	user := model.User{
		Username: "unsafe-quota-user",
		Password: "password123",
		Quota:    100,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&user).Error)
	payload, err := common.Marshal(ManageRequest{
		Id:     user.Id,
		Action: "add_quota",
		Mode:   "override",
		Value:  maxUserQuotaValue + 1,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("role", common.RoleRootUser)

	ManageUser(ctx)

	require.Contains(t, recorder.Body.String(), `"success":false`)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.EqualValues(t, 100, stored.Quota)
}

func TestManageUserRejectsMissingIdWithoutTouchingFirstUser(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	user := model.User{
		Username: "manage-missing-id-first-user",
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(
		`{"action":"disable"}`,
	))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("role", common.RoleRootUser)

	ManageUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, common.UserStatusEnabled, stored.Status)
}
