package model

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type failingAuthFlowReader struct{}

func (failingAuthFlowReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func setupAuthFlowTestDB(t *testing.T) {
	t.Helper()
	originalDB := DB
	db, err := gorm.Open(sqlite.Open("file:auth_flow_test?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&AuthFlow{}, &User{}, &UserOAuthBinding{}))
	require.True(t, db.Migrator().HasColumn(&AuthFlow{}, "session_id"))
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestAuthFlowIsProviderBoundAndConsumedOnce(t *testing.T) {
	setupAuthFlowTestDB(t)
	token, created, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeOAuth, Provider: "github", Intent: AuthFlowIntentLogin,
		SessionId: "session-a",
		Payload:   `{"affiliate_code":"invite"}`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.Len(t, token, 43)
	require.NotEqual(t, token, created.TokenHash)

	_, err = GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "discord"})
	require.ErrorIs(t, err, ErrAuthFlowInvalid)
	_, err = GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "github", SessionId: "session-b"})
	require.ErrorIs(t, err, ErrAuthFlowInvalid)

	const consumers = 2
	errorsByConsumer := make([]error, consumers)
	var wg sync.WaitGroup
	for i := 0; i < consumers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errorsByConsumer[index] = ConsumeAuthFlow(token, AuthFlowMatch{
				Purpose: AuthFlowPurposeOAuth, Provider: "github", Intent: AuthFlowIntentLogin, SessionId: "session-a",
			})
		}(i)
	}
	wg.Wait()

	successes := 0
	consumed := 0
	for _, consumeErr := range errorsByConsumer {
		switch {
		case consumeErr == nil:
			successes++
		case errors.Is(consumeErr, ErrAuthFlowConsumed):
			consumed++
		default:
			t.Fatalf("unexpected consume error: %v", consumeErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, consumed)
}

func TestAuthFlowExpiryIsEnforced(t *testing.T) {
	setupAuthFlowTestDB(t)
	token, flow, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeOAuth, Provider: "oidc", Intent: AuthFlowIntentLogin,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&AuthFlow{}).Where("id = ?", flow.Id).Update("expires_at", time.Now().Add(-time.Second)).Error)

	_, err = ConsumeAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "oidc"})
	require.ErrorIs(t, err, ErrAuthFlowExpired)
}

func TestGenerateAuthFlowTokenReturnsEntropyFailure(t *testing.T) {
	token, err := generateAuthFlowToken(failingAuthFlowReader{})
	require.Empty(t, token)
	require.ErrorContains(t, err, "entropy unavailable")
}

func TestExternalAuthAssertionCanOnlyBeClaimedOnce(t *testing.T) {
	setupAuthFlowTestDB(t)
	expiresAt := time.Now().Add(time.Minute)

	require.NoError(t, ClaimExternalAuthAssertion(AuthFlowPurposeTelegramAssertion, "signed-assertion", expiresAt))
	require.ErrorIs(t, ClaimExternalAuthAssertion(AuthFlowPurposeTelegramAssertion, "signed-assertion", expiresAt), ErrAuthFlowConsumed)
	require.NoError(t, ClaimExternalAuthAssertion(AuthFlowPurposeTelegramAssertion, "different-assertion", expiresAt))
}

func TestConsumeAuthFlowWithActionRollsBackTogether(t *testing.T) {
	setupAuthFlowTestDB(t)
	token, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: 42, SessionId: "session-a",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	actionErr := errors.New("binding failed")

	_, err = ConsumeAuthFlowWithAction(token, AuthFlowMatch{
		Purpose: AuthFlowPurposeTelegramBind, UserId: 42, SessionId: "session-a",
	}, func(tx *gorm.DB, _ *AuthFlow) error {
		if err := ClaimExternalAuthAssertionWithTx(tx, AuthFlowPurposeTelegramAssertion, "assertion-a", time.Now().Add(time.Minute)); err != nil {
			return err
		}
		return actionErr
	})
	require.ErrorIs(t, err, actionErr)

	flow, err := GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	require.Nil(t, flow.ConsumedAt)
	require.NoError(t, ClaimExternalAuthAssertion(AuthFlowPurposeTelegramAssertion, "assertion-a", time.Now().Add(time.Minute)))
}

func TestConsumeAuthFlowWithActionRollsBackOAuthBindingMutation(t *testing.T) {
	setupAuthFlowTestDB(t)
	user := User{Id: 43, Username: "oauth-bind-rollback-user", Password: "password-hash", Role: 1, Status: 1, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	token, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeOAuth, Provider: "custom", Intent: AuthFlowIntentBind,
		UserId: user.Id, SessionId: "session-a", ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	actionErr := errors.New("binding commit failed")

	_, err = ConsumeAuthFlowWithAction(token, AuthFlowMatch{
		Purpose: AuthFlowPurposeOAuth, Provider: "custom", Intent: AuthFlowIntentBind,
		UserId: user.Id, SessionId: "session-a",
	}, func(tx *gorm.DB, _ *AuthFlow) error {
		if err := UpdateUserOAuthBindingWithTx(tx, user.Id, 77, "provider-user-77"); err != nil {
			return err
		}
		return actionErr
	})
	require.ErrorIs(t, err, actionErr)

	var bindingCount int64
	require.NoError(t, DB.Model(&UserOAuthBinding{}).Count(&bindingCount).Error)
	require.Zero(t, bindingCount)
	flow, err := GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "custom", Intent: AuthFlowIntentBind, UserId: user.Id})
	require.NoError(t, err)
	require.Nil(t, flow.ConsumedAt)
}

func TestConsumeAuthFlowWithActionCommitsTogether(t *testing.T) {
	setupAuthFlowTestDB(t)
	token, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: 44, SessionId: "session-a",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)

	match := AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind, UserId: 44, SessionId: "session-a"}
	flow, err := ConsumeAuthFlowWithAction(token, match, func(tx *gorm.DB, _ *AuthFlow) error {
		return ClaimExternalAuthAssertionWithTx(tx, AuthFlowPurposeTelegramAssertion, "assertion-b", time.Now().Add(time.Minute))
	})
	require.NoError(t, err)
	require.NotNil(t, flow.ConsumedAt)

	_, err = ConsumeAuthFlowWithAction(token, match, nil)
	require.ErrorIs(t, err, ErrAuthFlowConsumed)
	require.ErrorIs(t, ClaimExternalAuthAssertion(AuthFlowPurposeTelegramAssertion, "assertion-b", time.Now().Add(time.Minute)), ErrAuthFlowConsumed)
}

func TestAuthFlowAssertionConflictClauseAvoidsMySQLPrimaryKeyNoop(t *testing.T) {
	mysqlClause := authFlowAssertionConflictClause("mysql")
	require.False(t, mysqlClause.DoNothing)
	require.Len(t, mysqlClause.DoUpdates, 1)
	require.Equal(t, "token_hash", mysqlClause.DoUpdates[0].Column.Name)

	conn, err := sql.Open("mysql", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
	})
	require.NoError(t, err)
	statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Clauses(authFlowAssertionConflictClause(tx.Dialector.Name())).Create(&AuthFlow{
			TokenHash: "assertion-hash", Purpose: AuthFlowPurposeTelegramAssertion, ExpiresAt: time.Now().Add(time.Minute),
		})
	})
	require.Contains(t, statement, "ON DUPLICATE KEY UPDATE `token_hash`=VALUES(`token_hash`)")
	require.NotContains(t, strings.ToLower(statement), "update `id`=")

	for _, dialect := range []string{"sqlite", "postgres"} {
		require.True(t, authFlowAssertionConflictClause(dialect).DoNothing)
	}
}

func TestMySQLExternalAssertionClaimOwnershipDoesNotTrustRowsAffected(t *testing.T) {
	setupAuthFlowTestDB(t)
	stored := AuthFlow{
		TokenHash: "stored-assertion-hash", Purpose: AuthFlowPurposeTelegramAssertion,
		Payload: "first-claim", ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, DB.Create(&stored).Error)

	claimed, err := ownsExternalAuthAssertionClaim(DB, "mysql", stored.TokenHash, "second-claim", 1)
	require.NoError(t, err)
	require.False(t, claimed)

	claimed, err = ownsExternalAuthAssertionClaim(DB, "mysql", stored.TokenHash, stored.Payload, 0)
	require.NoError(t, err)
	require.True(t, claimed)
}

func TestBindTelegramIdentityRejectsMismatchedSessionWithoutConsumingFlow(t *testing.T) {
	setupAuthFlowTestDB(t)
	user := User{Id: 42, Username: "telegram-session-user", Password: "password-hash", Role: 1, Status: 1, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	token, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeOAuth, Provider: "telegram", Intent: AuthFlowIntentBind,
		UserId: 42, SessionId: "session-a", ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)

	_, err = BindTelegramIdentityWithAuthFlowAndSessionAndAssertion(
		42, "123456", token, "session-b", "telegram-assertion", time.Now().Add(time.Minute),
	)
	require.ErrorIs(t, err, ErrAuthFlowInvalid)

	var stored User
	require.NoError(t, DB.First(&stored, 42).Error)
	require.Empty(t, stored.TelegramId)
	flow, err := GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "telegram", Intent: AuthFlowIntentBind, UserId: 42})
	require.NoError(t, err)
	require.Nil(t, flow.ConsumedAt)
}
