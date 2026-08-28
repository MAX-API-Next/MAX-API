package model

import (
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func setupSecurityCredentialTestState(t *testing.T) {
	t.Helper()
	setupUserUpdateTestState(t)
	require.NoError(t, DB.AutoMigrate(
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&AuthFlow{},
	))
	clearSecurityCredentialTestState(t)
	t.Cleanup(func() {
		clearSecurityCredentialTestState(t)
	})
}

func clearSecurityCredentialTestState(t *testing.T) {
	t.Helper()
	for _, target := range []interface{}{
		&PasskeyCredential{},
		&TwoFABackupCode{},
		&TwoFA{},
		&AuthFlow{},
	} {
		require.NoError(t, DB.Unscoped().Where("1 = 1").Delete(target).Error)
	}
}

func TestPasswordSecurityChangesBumpGenerationAndRecoveryRevokesTokens(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:                8801,
		Username:          "password-security-user",
		Password:          "OldPassword123",
		Email:             "password-security@example.com",
		Role:              common.RoleCommonUser,
		Status:            common.UserStatusEnabled,
		SessionGeneration: 3,
	}
	user.SetAccessToken("management-access-token")
	require.NoError(t, DB.Create(&user).Error)

	loaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	loaded.Password = "ChangedPassword123"
	require.NoError(t, loaded.Update(true))
	require.EqualValues(t, 4, loaded.SessionGeneration)
	require.True(t, common.ValidatePasswordAndHash("ChangedPassword123", loaded.Password))

	tokens := []Token{
		{Id: 8811, UserId: user.Id, Name: "enabled", Key: "password-reset-enabled", Status: common.TokenStatusEnabled},
		{Id: 8812, UserId: user.Id, Name: "disabled", Key: "password-reset-disabled", Status: common.TokenStatusDisabled},
	}
	require.NoError(t, DB.Create(&tokens).Error)

	require.NoError(t, ResetUserPasswordByEmail(user.Email, "RecoveredPassword123"))

	var recovered User
	require.NoError(t, DB.First(&recovered, user.Id).Error)
	require.EqualValues(t, 5, recovered.SessionGeneration)
	require.Empty(t, recovered.GetAccessToken())
	require.True(t, common.ValidatePasswordAndHash("RecoveredPassword123", recovered.Password))

	var storedTokens []Token
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("id asc").Find(&storedTokens).Error)
	require.Len(t, storedTokens, 2)
	for _, token := range storedTokens {
		require.Equal(t, common.TokenStatusDisabled, token.Status)
	}
}

func TestPasskeyAndTwoFASecurityChangesBumpSessionGeneration(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:       8821,
		Username: "credential-generation-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	firstCredential := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "Y3JlZGVudGlhbC0x",
		PublicKey:    "cHVibGljLWtleS0x",
	}
	generation, err := ReplacePasskeyCredentialAndBumpSessionGeneration(firstCredential)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)

	replacement := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "Y3JlZGVudGlhbC0y",
		PublicKey:    "cHVibGljLWtleS0y",
	}
	generation, err = ReplacePasskeyCredentialAndBumpSessionGeneration(replacement)
	require.NoError(t, err)
	require.EqualValues(t, 2, generation)

	var credentials []PasskeyCredential
	require.NoError(t, DB.Unscoped().Where("user_id = ?", user.Id).Find(&credentials).Error)
	require.Len(t, credentials, 1)
	require.Equal(t, replacement.CredentialID, credentials[0].CredentialID)

	generation, err = DeletePasskeyAndBumpSessionGeneration(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 3, generation)

	twoFA := &TwoFA{UserId: user.Id, Secret: "TESTSECRET", IsEnabled: false}
	require.NoError(t, DB.Create(twoFA).Error)
	generation, err = twoFA.EnableAndBumpSessionGeneration()
	require.NoError(t, err)
	require.EqualValues(t, 4, generation)

	generation, err = ReplaceBackupCodesAndBumpSessionGeneration(user.Id, []string{"ABCD-EFGH"})
	require.NoError(t, err)
	require.EqualValues(t, 5, generation)
	var backupCode TwoFABackupCode
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&backupCode).Error)
	require.NotEqual(t, "ABCD-EFGH", backupCode.CodeHash)
	require.True(t, common.ValidatePasswordAndHash("ABCD-EFGH", backupCode.CodeHash))
	valid, err := ValidateBackupCode(user.Id, "ABCD-EFGH")
	require.NoError(t, err)
	require.True(t, valid)
	valid, err = ValidateBackupCode(user.Id, "ABCD-EFGH")
	require.NoError(t, err)
	require.False(t, valid)

	generation, err = DisableTwoFAAndBumpSessionGeneration(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 6, generation)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.EqualValues(t, 6, stored.SessionGeneration)
}

func TestPasskeyUsageUpdateCannotRestoreReplacedCredential(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:       8822,
		Username: "passkey-usage-race-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	staleCredential := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "c3RhbGUtY3JlZGVudGlhbA==",
		PublicKey:    "c3RhbGUtcHVibGljLWtleQ==",
		SignCount:    1,
	}
	_, err := ReplacePasskeyCredentialAndBumpSessionGeneration(staleCredential)
	require.NoError(t, err)
	staleUsageUpdate := *staleCredential

	replacement := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "cmVwbGFjZW1lbnQtY3JlZGVudGlhbA==",
		PublicKey:    "cmVwbGFjZW1lbnQtcHVibGljLWtleQ==",
		SignCount:    10,
	}
	_, err = ReplacePasskeyCredentialAndBumpSessionGeneration(replacement)
	require.NoError(t, err)

	now := time.Now()
	staleUsageUpdate.SignCount = 2
	staleUsageUpdate.LastUsedAt = &now
	require.ErrorIs(t, UpdatePasskeyCredentialAfterAuthentication(&staleUsageUpdate), ErrPasskeyCredentialChanged)

	stored, err := GetPasskeyByUserID(user.Id)
	require.NoError(t, err)
	require.Equal(t, replacement.CredentialID, stored.CredentialID)
	require.Equal(t, replacement.PublicKey, stored.PublicKey)
	require.EqualValues(t, 10, stored.SignCount)

	later := now.Add(time.Second)
	matchingUsageUpdate := *replacement
	matchingUsageUpdate.ID = 0
	matchingUsageUpdate.SignCount = 11
	matchingUsageUpdate.LastUsedAt = &later
	require.NoError(t, UpdatePasskeyCredentialAfterAuthentication(&matchingUsageUpdate))
	stored, err = GetPasskeyByUserID(user.Id)
	require.NoError(t, err)
	require.Equal(t, replacement.CredentialID, stored.CredentialID)
	require.EqualValues(t, 11, stored.SignCount)
	require.WithinDuration(t, later, *stored.LastUsedAt, time.Second)
}

func TestTelegramBindingStateIsUserBoundAndConsumedOnce(t *testing.T) {
	setupSecurityCredentialTestState(t)

	owner := User{Id: 8831, Username: "telegram-owner", AffCode: "telegram-owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	other := User{Id: 8832, Username: "telegram-other", AffCode: "telegram-other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&other).Error)

	state, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose:   AuthFlowPurposeOAuth,
		Provider:  "telegram",
		Intent:    AuthFlowIntentBind,
		UserId:    owner.Id,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)

	_, err = BindTelegramIdentityWithAuthFlow(other.Id, "123456", state)
	require.ErrorIs(t, err, ErrAuthFlowInvalid)

	generation, err := BindTelegramIdentityWithAuthFlow(owner.Id, "123456", state)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)

	_, err = BindTelegramIdentityWithAuthFlow(owner.Id, "123456", state)
	require.ErrorIs(t, err, ErrAuthFlowConsumed)

	var stored User
	require.NoError(t, DB.First(&stored, owner.Id).Error)
	require.Equal(t, "123456", stored.TelegramId)
	require.EqualValues(t, 1, stored.SessionGeneration)
}
